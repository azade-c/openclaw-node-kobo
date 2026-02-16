package gateway

import (
	"encoding/json"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Envelope struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *RPCError        `json:"error,omitempty"`
}

type InvokeRequestParams struct {
	RequestID string          `json:"requestId,omitempty"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
}

type InvokeResultParams struct {
	RequestID string      `json:"requestId,omitempty"`
	Result    interface{} `json:"result,omitempty"`
	Error     *RPCError   `json:"error,omitempty"`
}

type NodeRegistration struct {
	Role     string   `json:"role"`
	Caps     []string `json:"caps"`
	Commands []string `json:"commands"`
}

type EventParams struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}
