// Package feishu 处理飞书事件接收与验证
package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ─── 飞书 Webhook 事件结构 ────────────────────────────────────

// RawEvent 飞书 HTTP 事件原始结构
type RawEvent struct {
	Schema string          `json:"schema"` // "2.0"
	Header *EventHeader    `json:"header"`
	Event  json.RawMessage `json:"event"`
	// v1 challenge（飞书首次验证 webhook 时发送）
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
}

type EventHeader struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"` // im.message.receive_v1
	CreateTime string `json:"create_time"`
	Token      string `json:"token"`
	AppID      string `json:"app_id"`
	TenantKey  string `json:"tenant_key"`
}

// MessageEvent im.message.receive_v1 事件体
type MessageEvent struct {
	Sender  *MessageSender  `json:"sender"`
	Message *MessageContent `json:"message"`
}

type MessageSender struct {
	SenderID   *UserID `json:"sender_id"`
	TenantKey  string  `json:"tenant_key"`
	SenderType string  `json:"sender_type"` // user
}

type UserID struct {
	OpenID  string `json:"open_id"`
	UnionID string `json:"union_id"`
	UserID  string `json:"user_id"`
}

type MessageContent struct {
	MessageID   string   `json:"message_id"`
	RootID      string   `json:"root_id"`
	ParentID    string   `json:"parent_id"`
	CreateTime  string   `json:"create_time"`
	UpdateTime  string   `json:"update_time"`
	ChatID      string   `json:"chat_id"`
	ChatType    string   `json:"chat_type"` // group | p2p
	MessageType string   `json:"message_type"` // text | post | ...
	Content     string   `json:"content"` // JSON 字符串
	Mentions    []*Mention `json:"mentions"`
}

type Mention struct {
	Key    string  `json:"key"`
	ID     *UserID `json:"id"`
	Name   string  `json:"name"`
	TenantKey string `json:"tenant_key"`
}

// TextContent 文本消息 content 解析
type TextContent struct {
	Text string `json:"text"`
}

// ─── 签名验证 ─────────────────────────────────────────────────

// VerifySignature 验证飞书 webhook 签名
// 签名算法：SHA256(timestamp + nonce + encrypt_key + body)
func VerifySignature(timestamp, nonce, encryptKey, body, signature string) bool {
	s := timestamp + nonce + encryptKey + body
	h := sha256.New()
	h.Write([]byte(s))
	expected := fmt.Sprintf("%x", h.Sum(nil))
	return expected == signature
}

// ─── 加密消息解密 ─────────────────────────────────────────────

// EncryptedBody 加密事件体
type EncryptedBody struct {
	Encrypt string `json:"encrypt"`
}

// Decrypt 解密飞书加密消息（AES-256-CBC）
func Decrypt(encrypt, encryptKey string) ([]byte, error) {
	// key = SHA256(encryptKey)[:32]
	h := sha256.New()
	h.Write([]byte(encryptKey))
	key := h.Sum(nil) // 32 字节

	ciphertext, err := base64.StdEncoding.DecodeString(encrypt)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// 去掉 PKCS7 padding
	return pkcs7Unpad(ciphertext)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	n := len(data)
	if n == 0 {
		return nil, fmt.Errorf("empty data")
	}
	pad := int(data[n-1])
	if pad > aes.BlockSize || pad == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	return data[:n-pad], nil
}

// ─── 消息解析 ─────────────────────────────────────────────────

// ParseTextContent 从 MessageContent.Content（JSON 字符串）中提取文本
func ParseTextContent(content string) string {
	var tc TextContent
	if err := json.Unmarshal([]byte(content), &tc); err != nil {
		return content
	}
	// 去掉 @ 机器人的标记（如 @_user_1 ）
	text := tc.Text
	// 飞书文本中 @ 用户格式为 @<mention_key>，替换掉
	// 保留空格，方便后续处理
	return strings.TrimSpace(text)
}

// ReadBody 读取 HTTP 请求体（复用安全）
func ReadBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 最大 1MB
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
