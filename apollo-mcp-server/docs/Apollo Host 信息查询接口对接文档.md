# Apollo Host 信息查询接口对接文档

## 1. 接口概述

| 项目       | 说明                          |
| -------- | --------------------------- |
| **接口名称** | 获取 Apollo Host 信息           |
| **接口地址** | https://easyops.tech.ctseelink.cn  |
| **接口路径** | `/api/getApolloHostInfo`    |
| **请求方法** | `GET`                       |
| **接口类型** | 第三方外部接口                     |
| **认证方式** | 第三方访问认证（`AuthRequireThird`） |

## 2. 认证说明

### 2.1 请求凭证

第三方调用本接口需携带以下认证信息：

| 参数 | 位置 | 类型 | 必填 | 说明 |
|------|------|------|------|------|
| `sessionId` | Cookie | string | 是 | 32 位字符的会话标识，用于身份认证及 token 字段加密 |

## 3. 请求参数

所有参数通过 Query String 传递：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `paginator` | bool | 否 | `true` | 是否分页查询 |
| `pageIndex` | int | 否 | `1` | 页码（分页模式下生效） |
| `pageSize` | int | 否 | `10` | 每页条数（分页模式下生效） |
| `name` | string | 否 | - | 服务名称，模糊查询 |
| `secondProductId` | list | 否 | - | 关联产品 ID 列表，多值匹配 |
| `host` | string | 否 | - | 服务地址，模糊查询 |
| `user` | string | 否 | - | 访问用户，模糊查询 |
| `token` | string | 否 | - | Token，模糊查询 |

### 请求示例

```http
GET /api/getApolloHostInfo?pageIndex=1&pageSize=10&name=apollo-test HTTP/1.1
Host: <平台域名>
Cookie: sessionId=abcdef1234567890abcdef1234567890
```

## 4. 响应说明

### 4.1 成功响应

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | string | 固定为 `"success"` |
| `list` | array | Apollo Host 信息列表 |
| `pageTotal` | int | 总记录数 |

**`list` 中每个元素的字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int | 记录主键 ID |
| `del_flag` | bool | 删除标记，固定为 `false` |
| `create_time` | string | 创建时间，格式 `YYYY-MM-DD HH:mm:ss` |
| `update_time` | string | 更新时间，格式 `YYYY-MM-DD HH:mm:ss` |
| `name` | string | 服务名称 |
| `secondProductId` | list | 关联产品 ID 列表 |
| `host` | string | Apollo 服务地址 |
| `user` | string | 访问用户 |
| `token` | string | **已加密**的 Token（加密方式见第 5 节） |
| `operation` | string | 操作信息 |
| `operator` | int | 操作人 ID |

### 4.2 成功响应示例

```json
{
    "code": "success",
    "list": [
        {
            "id": 15,
            "del_flag": false,
            "create_time": "2026-05-29 10:31:20",
            "update_time": "2026-05-29 10:38:51",
            "name": "天翼云眼贵州测试Apollo-亿讯专用",
            "secondProductId": ["518"],
            "host": "https://tyyy-guizh-apollo-test.ctseelink.cn:8154",
            "user": "eshore-user",
            "token": "Gw8CBg4ADgIOBg4ADgIOBg4ADgIOBg==",
            "operation": "",
            "operator": 0
        }
    ],
    "pageTotal": 1
}
```

### 4.3 失败响应

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | string | 固定为 `"fail"` |
| `message` | string | 错误描述 |

```json
{
    "code": "fail",
    "message": "未记录的访问"
}
```

**常见错误：**

| message | 原因 |
|---------|------|
| `未记录的访问` | 路径未在 `ThirdPartyAccess` 表中注册 |
| `未认证的IP访问!` | 调用方 IP 不在白名单内 |
| `数据异常，请联系管理员` | 服务端处理异常 |

## 5. Token 字段加密说明

### 5.1 加密算法

返回数据中的 `token` 字段使用请求携带的 `sessionId`（32 位字符）作为密钥进行加密，算法如下：

1. 将原始 `token` 的每个字符与 `sessionId` 循环逐字节 **XOR（异或）**
2. 将异或后的字节数组进行 **Base64 编码**

### 5.2 加密伪代码

```
encrypted_bytes[i] = token[i] XOR sessionId[i mod len(sessionId)]
encrypted_token = Base64Encode(encrypted_bytes)
```

### 5.3 解密方法

第三方拿到加密后的 `token` 后，使用相同的 `sessionId` 解密：

1. 对 `token` 字段值进行 **Base64 解码**，得到字节数组
2. 将每个字节与 `sessionId` 循环逐字节 **XOR**，还原原始 token

### 5.4 代码示例

**Python：**

```python
import base64

def decrypt_token(encrypted_token, session_id):
    """解密 token"""
    decoded = base64.b64decode(encrypted_token)
    original = ''.join(
        chr(decoded[i] ^ ord(session_id[i % len(session_id)]))
        for i in range(len(decoded))
    )
    return original

def encrypt_token(token, session_id):
    """加密 token（与服务端逻辑一致）"""
    xor_bytes = bytearray(
        ord(token[i]) ^ ord(session_id[i % len(session_id)])
        for i in range(len(token))
    )
    return base64.b64encode(bytes(xor_bytes)).decode('utf-8')

# 示例
session_id = "abcdef1234567890abcdef1234567890"  # 32位
raw_token = "0a7abe979990sssss"

encrypted = encrypt_token(raw_token, session_id)
print("加密后:", encrypted)

decrypted = decrypt_token(encrypted, session_id)
print("解密后:", decrypted)
# 输出: 0a7abe979990sssss
```

**Java：**

```java
import java.util.Base64;

public class TokenDecrypt {
    public static String decryptToken(String encryptedToken, String sessionId) {
        byte[] decoded = Base64.getDecoder().decode(encryptedToken);
        char[] sessionChars = sessionId.toCharArray();
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < decoded.length; i++) {
            sb.append((char) (decoded[i] ^ sessionChars[i % sessionChars.length]));
        }
        return sb.toString();
    }

    public static void main(String[] args) {
        String sessionId = "abcdef1234567890abcdef1234567890";
        String encryptedToken = "Gw8CBg4ADgIOBg4ADgIOBg4ADgIOBg==";
        String token = decryptToken(encryptedToken, sessionId);
        System.out.println("解密后: " + token);
    }
}
```

## 6. 注意事项

1. **sessionId 安全**：`sessionId` 既是认证凭证也是加密密钥，请妥善保管，不要在 URL 或日志中暴露
2. **token 字段**：返回的 `token` 为加密后值，需使用 `sessionId` 解密后才能获得原始 Token