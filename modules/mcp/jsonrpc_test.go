package mcp

import "testing"

func TestCodecEncodeRequest(t *testing.T) {
	c := NewCodec()
	data, id := c.EncodeRequest("initialize", nil)
	if id != 1 {
		t.Fatalf("expected id 1, got %d", id)
	}
	if len(data) == 0 {
		t.Fatal("empty request")
	}
}

func TestCodecDecodeResponse(t *testing.T) {
	c := NewCodec()
	resp, err := c.DecodeResponse([]byte(`{"jsonrpc":"2.0","result":{"tools":[]},"id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != 1 {
		t.Fatalf("expected id 1, got %d", resp.ID)
	}
}

func TestCodecDecodeError(t *testing.T) {
	c := NewCodec()
	resp, _ := c.DecodeResponse([]byte(`{"jsonrpc":"2.0","error":{"code":-1,"message":"fail"},"id":1}`))
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Message != "fail" {
		t.Fatalf("expected fail, got %q", resp.Error.Message)
	}
}

func TestCodecIDIncrement(t *testing.T) {
	c := NewCodec()
	_, id1 := c.EncodeRequest("a", nil)
	_, id2 := c.EncodeRequest("b", nil)
	if id2 != id1+1 {
		t.Fatalf("expected sequential IDs")
	}
}
