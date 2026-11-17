package topo

import (
	"encoding/json"
	"errors"
	"net/url"

	"aiops-platform/backend/internal/domain"
)

// ErrOpSyncNotSupported file 快照模式为只读数据源，无采集链路。
var ErrOpSyncNotSupported = errors.New("file 快照模式为只读数据源，不支持手动触发同步（TOPO_MODE=http 对接 topo 服务后可用）")

// OpSyncer 同步能力：file 模式返回 ErrOpSyncNotSupported；http 模式代理 topo 服务。
type OpSyncer interface {
	OpTriggerSync(trigger string) (started bool, msg string, state *domain.OpSyncState, err error)
	OpSyncStatus() *domain.OpSyncState
	OpSyncLogs(date, download string) (contentType string, body []byte, err error)
}

// OpProxy http 模式下对 topo 服务原生接口的透传代理（path 为服务侧完整路径）。
type OpProxy interface {
	OpGet(path string, q url.Values) ([]byte, error)
	OpPost(path string) ([]byte, error)
}

// OpBasePath topo 服务运维关系拓扑接口前缀。
const OpBasePath = "/api/external/operationTopology"

// ---------- FileProvider：只读占位实现 ----------

func (p *FileProvider) OpTriggerSync(trigger string) (bool, string, *domain.OpSyncState, error) {
	return false, "", &domain.OpSyncState{TriggerBy: trigger}, ErrOpSyncNotSupported
}

func (p *FileProvider) OpSyncStatus() *domain.OpSyncState {
	return &domain.OpSyncState{}
}

func (p *FileProvider) OpSyncLogs(string, string) (string, []byte, error) {
	return "", nil, ErrOpSyncNotSupported
}

// ---------- HTTPProvider：代理 topo 服务 ----------

func (p *HTTPProvider) OpGet(path string, q url.Values) ([]byte, error) {
	return p.getRaw(path, q)
}

func (p *HTTPProvider) OpPost(path string) ([]byte, error) {
	return p.postRaw(path)
}

func (p *HTTPProvider) OpTriggerSync(trigger string) (bool, string, *domain.OpSyncState, error) {
	body, err := p.postRaw(OpBasePath + "/sync")
	if err != nil {
		return false, "", nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Started bool               `json:"started"`
			Message string             `json:"message"`
			State   domain.OpSyncState `json:"state"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, "", nil, err
	}
	if resp.Code != 0 && resp.Code != 200 {
		return false, "", nil, errors.New("topo 服务返回 code=" + itoa(resp.Code) + ": " + resp.Msg)
	}
	st := resp.Data.State
	st.Supported = true
	if st.TriggerBy == "" {
		st.TriggerBy = trigger
	}
	return resp.Data.Started, resp.Data.Message, &st, nil
}

func (p *HTTPProvider) OpSyncStatus() *domain.OpSyncState {
	body, err := p.getRaw(OpBasePath+"/sync/status", nil)
	if err != nil {
		return &domain.OpSyncState{Supported: true, Error: err.Error()}
	}
	var resp struct {
		Code int                `json:"code"`
		Data domain.OpSyncState `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return &domain.OpSyncState{Supported: true, Error: "解析同步状态失败"}
	}
	st := resp.Data
	st.Supported = true
	return &st
}

func (p *HTTPProvider) OpSyncLogs(date, download string) (string, []byte, error) {
	q := url.Values{}
	if date != "" {
		q.Set("date", date)
	}
	if download != "" {
		q.Set("download", download)
	}
	body, err := p.getRaw(OpBasePath+"/sync/logs", q)
	if err != nil {
		return "", nil, err
	}
	if download == "1" || equalFold(download, "true") {
		return "text/plain; charset=utf-8", body, nil
	}
	return "application/json", body, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
