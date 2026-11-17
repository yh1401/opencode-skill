package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store 平台文档存储抽象：文件为主，Postgres 为备份/兜底。
// 文档按 (docType, docID) 二元组定位，payload 为任意 JSON 对象。
type Store interface {
	Put(docType, docID string, v any) error
	Get(docType, docID string) (json.RawMessage, bool)
	List(docType string) []json.RawMessage
	Delete(docType, docID string) error
	Info() Info
}

// Info 存储运行状态（供 /api/meta 展示）。
type Info struct {
	Dir         string `json:"dir"`
	Files       int    `json:"files"`
	Postgres    bool   `json:"postgres"`
	PostgresDSN string `json:"-"`
	PGStatus    string `json:"pgStatus"`
}

// FileStore 以 JSON 文件为主存储：<dir>/<docType>/<docID>.json。
// 写入顺序：先原子写文件（主），再 best-effort 镜像到 PG（备份）；
// 读取顺序：先文件，文件缺失时回退 PG（兜底）。
type FileStore struct {
	dir string
	pg  *pgMirror
	mu  sync.RWMutex
}

func NewFileStore(dir string, dsn string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}
	fs := &FileStore{dir: dir}
	if strings.TrimSpace(dsn) != "" {
		pg, err := newPGMirror(dsn)
		if err != nil {
			// PG 是备份，不阻塞启动；后续写入时按间隔重试
			fs.pg = &pgMirror{dsn: dsn, lastErr: err.Error()}
		} else {
			fs.pg = pg
		}
	}
	return fs, nil
}

func (s *FileStore) safeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "empty"
	}
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_").Replace(id)
}

func (s *FileStore) path(docType, docID string) string {
	return filepath.Join(s.dir, s.safeID(docType), s.safeID(docID)+".json")
}

func (s *FileStore) Put(docType, docID string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p := s.path(docType, docID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	s.mu.RLock()
	pg := s.pg
	s.mu.RUnlock()
	if pg != nil {
		_ = pg.Upsert(docType, docID, raw) // 备份失败不影响主流程
	}
	return nil
}

func (s *FileStore) Get(docType, docID string) (json.RawMessage, bool) {
	if raw, err := os.ReadFile(s.path(docType, docID)); err == nil {
		return json.RawMessage(raw), true
	}
	s.mu.RLock()
	pg := s.pg
	s.mu.RUnlock()
	if pg != nil {
		if raw, ok, err := pg.Get(docType, docID); err == nil && ok {
			// 兜底命中后回填本地文件，恢复“文件为主”
			p := s.path(docType, docID)
			if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
				_ = os.WriteFile(p, raw, 0o644)
			}
			return json.RawMessage(raw), true
		}
	}
	return nil, false
}

func (s *FileStore) List(docType string) []json.RawMessage {
	dir := filepath.Join(s.dir, s.safeID(docType))
	entries, err := os.ReadDir(dir)
	out := []json.RawMessage{}
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if raw, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
				out = append(out, json.RawMessage(raw))
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	s.mu.RLock()
	pg := s.pg
	s.mu.RUnlock()
	if pg != nil {
		if rows, err := pg.List(docType); err == nil {
			for _, r := range rows {
				out = append(out, json.RawMessage(r))
			}
			return out
		}
	}
	return out
}

func (s *FileStore) Delete(docType, docID string) error {
	err := os.Remove(s.path(docType, docID))
	s.mu.RLock()
	pg := s.pg
	s.mu.RUnlock()
	if pg != nil {
		_ = pg.Delete(docType, docID)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *FileStore) Info() Info {
	info := Info{Dir: s.dir}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if files, err := os.ReadDir(filepath.Join(s.dir, e.Name())); err == nil {
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".json") {
					info.Files++
				}
			}
		}
	}
	s.mu.RLock()
	pg := s.pg
	s.mu.RUnlock()
	if pg != nil {
		info.Postgres = true
		info.PGStatus = pg.Status()
	} else {
		info.PGStatus = "disabled"
	}
	return info
}

// GetInto 读取并反序列化，缺失时返回 false。
func GetInto[T any](s Store, docType, docID string, out *T) bool {
	raw, ok := s.Get(docType, docID)
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// ListInto 批量反序列化，无法解析的文档会被跳过。
func ListInto[T any](s Store, docType string) []T {
	raws := s.List(docType)
	out := make([]T, 0, len(raws))
	for _, raw := range raws {
		var v T
		if json.Unmarshal(raw, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// NewID 生成带时间前缀的文档 ID（文件按名排序即时间序）。
func NewID(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, time.Now().Format("20060102-150405.000"))
}
