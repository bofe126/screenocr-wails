package translator

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TencentTranslator 腾讯云翻译器
type TencentTranslator struct {
	secretID  string
	secretKey string
	endpoint  string
	service   string
	version   string
	region    string
}

// TranslateRequest 翻译请求
type TranslateRequest struct {
	SourceText string `json:"SourceText"`
	Source     string `json:"Source"`
	Target     string `json:"Target"`
	ProjectId  int    `json:"ProjectId"`
}

// TranslateResponse 翻译响应
type TranslateResponse struct {
	Response struct {
		TargetText string `json:"TargetText"`
		Source     string `json:"Source"`
		Target     string `json:"Target"`
		RequestId  string `json:"RequestId"`
		Error      *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

// NewTencentTranslator 创建翻译器
func NewTencentTranslator(secretID, secretKey string) *TencentTranslator {
	return &TencentTranslator{
		secretID:  secretID,
		secretKey: secretKey,
		endpoint:  "tmt.tencentcloudapi.com",
		service:   "tmt",
		version:   "2018-03-21",
		region:    "ap-guangzhou",
	}
}

// SetCredentials 设置凭证
func (t *TencentTranslator) SetCredentials(secretID, secretKey string) {
	t.secretID = secretID
	t.secretKey = secretKey
}

// IsConfigured 检查是否已配置
func (t *TencentTranslator) IsConfigured() bool {
	return t.secretID != "" && t.secretKey != ""
}

// Translate 翻译文本
func (t *TencentTranslator) Translate(text, source, target string) (string, error) {
	if !t.IsConfigured() {
		return "", fmt.Errorf("翻译 API 未配置")
	}

	// 准备请求
	reqBody := TranslateRequest{
		SourceText: text,
		Source:     source,
		Target:     target,
		ProjectId:  0,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	// 生成签名
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	authorization := t.sign(bodyBytes, timestamp, date)

	// 发送请求
	req, err := http.NewRequest("POST", fmt.Sprintf("https://%s", t.endpoint), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", t.endpoint)
	req.Header.Set("X-TC-Action", "TextTranslate")
	req.Header.Set("X-TC-Version", t.version)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Region", t.region)
	req.Header.Set("Authorization", authorization)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var result TranslateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查错误
	if result.Response.Error != nil {
		return "", fmt.Errorf("%s: %s", result.Response.Error.Code, result.Response.Error.Message)
	}

	return result.Response.TargetText, nil
}

// sign 生成 TC3-HMAC-SHA256 签名
func (t *TencentTranslator) sign(payload []byte, timestamp int64, date string) string {
	// 步骤1：拼接规范请求串
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:application/json; charset=utf-8\nhost:%s\n", t.endpoint)
	signedHeaders := "content-type;host"
	hashedRequestPayload := sha256Hex(payload)

	canonicalRequest := strings.Join([]string{
		httpRequestMethod,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedRequestPayload,
	}, "\n")

	// 步骤2：拼接待签名字符串
	algorithm := "TC3-HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, t.service)
	hashedCanonicalRequest := sha256Hex([]byte(canonicalRequest))

	stringToSign := strings.Join([]string{
		algorithm,
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		hashedCanonicalRequest,
	}, "\n")

	// 步骤3：计算签名
	secretDate := hmacSHA256([]byte("TC3"+t.secretKey), date)
	secretService := hmacSHA256(secretDate, t.service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	// 步骤4：拼接 Authorization
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, t.secretID, credentialScope, signedHeaders, signature)

	return authorization
}

// sha256Hex 计算 SHA256 哈希
func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hmacSHA256 计算 HMAC-SHA256
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// 支持的语言列表
var SupportedLanguages = map[string]string{
	"auto": "自动检测",
	"zh":   "中文",
	"en":   "英语",
	"ja":   "日语",
	"ko":   "韩语",
	"fr":   "法语",
	"de":   "德语",
	"es":   "西班牙语",
	"ru":   "俄语",
}

