package mcp

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

type Codec struct {
	nextID atomic.Int64
}

func NewCodec() *Codec { return &Codec{} }

func (c *Codec) EncodeRequest(method string, params any) ([]byte, int64) {
	id := c.nextID.Add(1)
	req := JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	return data, id
}

func (c *Codec) EncodeResponse(id int64, result any) []byte {
	resp := JSONRPCResponse{JSONRPC: "2.0", Result: result, ID: id}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	return data
}

func (c *Codec) EncodeError(id int64, code int, message string) []byte {
	resp := JSONRPCResponse{JSONRPC: "2.0", Error: &JSONRPCError{Code: code, Message: message}, ID: id}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	return data
}

func (c *Codec) DecodeResponse(data []byte) (*JSONRPCResponse, error) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC response: %w", err)
	}
	return &resp, nil
}

func (c *Codec) DecodeRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC request: %w", err)
	}
	return &req, nil
}
