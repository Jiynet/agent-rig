package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// StdioTransport 基于 stdin/stdout 的 JSON-RPC 传输层。
type StdioTransport struct {
	in  *bufio.Scanner
	out io.Writer
}

// NewStdioTransport 创建 stdio 传输层。
func NewStdioTransport(in io.Reader, out io.Writer) *StdioTransport {
	return &StdioTransport{
		in:  bufio.NewScanner(in),
		out: out,
	}
}

// Read 从 stdin 读取一行 JSON-RPC 请求。
func (t *StdioTransport) Read() (*Request, error) {
	if !t.in.Scan() {
		if err := t.in.Err(); err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return nil, io.EOF
	}

	data := t.in.Bytes()
	if len(data) == 0 {
		return nil, fmt.Errorf("empty message")
	}

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	return &req, nil
}

// Write 将 JSON-RPC 响应写入 stdout。
func (t *StdioTransport) Write(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	data = append(data, '\n')
	_, err = t.out.Write(data)
	return err
}
