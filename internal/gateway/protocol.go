package gateway

import "encoding/json"

const ProtocolVersion = 3

type RequestFrame struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ResponseFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *GatewayError   `json:"error,omitempty"`
}

type EventFrame struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type ShutdownPayload struct {
	Reason            string `json:"reason,omitempty"`
	RestartExpectedMs int    `json:"restartExpectedMs,omitempty"`
}

type GatewayError struct {
	Code         string          `json:"code,omitempty"`
	Message      string          `json:"message,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    *bool           `json:"retryable,omitempty"`
	RetryAfterMs *int            `json:"retryAfterMs,omitempty"`
}

type ClientInfo struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName,omitempty"`
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	DeviceFamily    string `json:"deviceFamily,omitempty"`
	ModelIdentifier string `json:"modelIdentifier,omitempty"`
	Mode            string `json:"mode"`
	InstanceID      string `json:"instanceId,omitempty"`
}

type ConnectAuth struct {
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
}

type ConnectParams struct {
	MinProtocol int             `json:"minProtocol"`
	MaxProtocol int             `json:"maxProtocol"`
	Client      ClientInfo      `json:"client"`
	Role        string          `json:"role,omitempty"`
	Caps        []string        `json:"caps,omitempty"`
	Commands    []string        `json:"commands,omitempty"`
	Permissions map[string]bool `json:"permissions,omitempty"`
	PathEnv     string          `json:"pathEnv,omitempty"`
	Scopes      []string        `json:"scopes,omitempty"`
	Auth        *ConnectAuth    `json:"auth,omitempty"`
	Device      *DeviceInfo     `json:"device,omitempty"`
	Locale      string          `json:"locale,omitempty"`
	UserAgent   string          `json:"userAgent,omitempty"`
}

type DeviceInfo struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce,omitempty"`
}

type NodeRegistration struct {
	Client      ClientInfo      `json:"client"`
	Role        string          `json:"role"`
	Caps        []string        `json:"caps"`
	Commands    []string        `json:"commands"`
	Permissions map[string]bool `json:"permissions,omitempty"`
	PathEnv     string          `json:"pathEnv,omitempty"`
	Scopes      []string        `json:"scopes,omitempty"`
	Locale      string          `json:"locale,omitempty"`
	UserAgent   string          `json:"userAgent,omitempty"`
}

type HelloOkPayload struct {
	Type string       `json:"type"`
	Auth *HelloOkAuth `json:"auth,omitempty"`
}

type HelloOkAuth struct {
	DeviceToken string   `json:"deviceToken,omitempty"`
	Role        string   `json:"role,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	IssuedAtMs  int64    `json:"issuedAtMs,omitempty"`
}

type InvokeRequestParams struct {
	RequestID string          `json:"id"`
	NodeID    string          `json:"nodeId"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
}

type InvokeResultParams struct {
	RequestID  string           `json:"id"`
	NodeID     string           `json:"nodeId"`
	OK         bool             `json:"ok"`
	Result     interface{}      `json:"payload,omitempty"`
	ResultJSON *string          `json:"payloadJSON,omitempty"`
	Error      *NodeInvokeError `json:"error,omitempty"`
}

type NodeInvokeError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type NodeEventParams struct {
	Event       string      `json:"event"`
	Payload     interface{} `json:"payload,omitempty"`
	PayloadJSON *string     `json:"payloadJSON,omitempty"`
}
