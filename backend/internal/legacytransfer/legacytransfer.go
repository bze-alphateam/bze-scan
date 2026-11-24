// Package legacytransfer is the one ibc-go v8 query the explorer still
// needs: transfer Query/DenomTrace, which a node answers until its network
// moves to ibc-go v10 (v8.3.0 of the chain), where Query/Denom replaces it.
//
// ibc-go v10 no longer carries the messages, and registering them next to
// v10's would clash on nothing but is not needed either: the three messages
// are hand-coded protobuf (field numbers of ibc-go v8.8.0
// proto/ibc/applications/transfer/v1/{query,transfer}.proto) and are never
// registered in any proto registry. They satisfy gogoproto.Message, so the
// chain's gRPC codec encodes them like any other.
package legacytransfer

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
)

// DenomTraceMethod is the full gRPC method of the old query.
const DenomTraceMethod = "/ibc.applications.transfer.v1.Query/DenomTrace"

// DenomTrace is ibc.applications.transfer.v1.DenomTrace: the path of port/
// channel pairs and the base denom.
type DenomTrace struct {
	Path      string
	BaseDenom string
}

// QueryDenomTraceRequest is the request: the hash of the trace, hex, without
// the "ibc/" prefix.
type QueryDenomTraceRequest struct {
	Hash string
}

// QueryDenomTraceResponse is the answer.
type QueryDenomTraceResponse struct {
	DenomTrace *DenomTrace
}

// Client is the old transfer query client.
type Client interface {
	DenomTrace(ctx context.Context, in *QueryDenomTraceRequest, opts ...grpc.CallOption) (*QueryDenomTraceResponse, error)
}

type client struct{ cc grpc.ClientConnInterface }

// NewClient returns a Client over cc.
func NewClient(cc grpc.ClientConnInterface) Client { return client{cc: cc} }

func (c client) DenomTrace(ctx context.Context, in *QueryDenomTraceRequest, opts ...grpc.CallOption) (*QueryDenomTraceResponse, error) {
	out := new(QueryDenomTraceResponse)
	if err := c.cc.Invoke(ctx, DenomTraceMethod, in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// Reset, String and ProtoMessage make the messages gogoproto.Message.

func (m *DenomTrace) Reset() { *m = DenomTrace{} }
func (m *DenomTrace) String() string {
	return fmt.Sprintf("path:%q base_denom:%q", m.Path, m.BaseDenom)
}
func (*DenomTrace) ProtoMessage() {}

func (m *QueryDenomTraceRequest) Reset()         { *m = QueryDenomTraceRequest{} }
func (m *QueryDenomTraceRequest) String() string { return fmt.Sprintf("hash:%q", m.Hash) }
func (*QueryDenomTraceRequest) ProtoMessage()    {}

func (m *QueryDenomTraceResponse) Reset() { *m = QueryDenomTraceResponse{} }
func (m *QueryDenomTraceResponse) String() string {
	return fmt.Sprintf("denom_trace:{%v}", m.DenomTrace)
}
func (*QueryDenomTraceResponse) ProtoMessage() {}

// Marshal encodes the trace: path = 1, base_denom = 2.
func (m *DenomTrace) Marshal() ([]byte, error) {
	var b []byte
	b = appendString(b, 1, m.Path)
	b = appendString(b, 2, m.BaseDenom)
	return b, nil
}

// Unmarshal decodes the trace.
func (m *DenomTrace) Unmarshal(b []byte) error {
	return walk(b, func(num protowire.Number, v []byte) {
		switch num {
		case 1:
			m.Path = string(v)
		case 2:
			m.BaseDenom = string(v)
		}
	})
}

// Marshal encodes the request: hash = 1.
func (m *QueryDenomTraceRequest) Marshal() ([]byte, error) {
	return appendString(nil, 1, m.Hash), nil
}

// Unmarshal decodes the request.
func (m *QueryDenomTraceRequest) Unmarshal(b []byte) error {
	return walk(b, func(num protowire.Number, v []byte) {
		if num == 1 {
			m.Hash = string(v)
		}
	})
}

// Marshal encodes the response: denom_trace = 1.
func (m *QueryDenomTraceResponse) Marshal() ([]byte, error) {
	if m.DenomTrace == nil {
		return nil, nil
	}
	inner, err := m.DenomTrace.Marshal()
	if err != nil {
		return nil, err
	}
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, inner), nil
}

// Unmarshal decodes the response.
func (m *QueryDenomTraceResponse) Unmarshal(b []byte) error {
	return walk(b, func(num protowire.Number, v []byte) {
		if num == 1 {
			t := &DenomTrace{}
			if err := t.Unmarshal(v); err == nil {
				m.DenomTrace = t
			}
		}
	})
}

func appendString(b []byte, num protowire.Number, s string) []byte {
	if s == "" {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

// walk calls fn with the payload of every length-delimited field of b and
// skips every other wire type.
func walk(b []byte, fn func(protowire.Number, []byte)) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
		if typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			fn(num, v)
			b = b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
	}
	return nil
}
