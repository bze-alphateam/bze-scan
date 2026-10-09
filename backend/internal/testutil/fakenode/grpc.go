package fakenode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// Services maps the short names of the query services the fake gRPC server
// answers (the directories under testdata/grpc/) to their full names.
var Services = map[string]string{
	"staking":        "cosmos.staking.v1beta1.Query",
	"slashing":       "cosmos.slashing.v1beta1.Query",
	"bank":           "cosmos.bank.v1beta1.Query",
	"distribution":   "cosmos.distribution.v1beta1.Query",
	"gov":            "cosmos.gov.v1.Query",
	"mint":           "cosmos.mint.v1beta1.Query",
	"transfer":       "ibc.applications.transfer.v1.Query",
	"channel":        "ibc.core.channel.v1.Query",
	"tradebin":       "bze.tradebin.Query",
	"tokenfactory":   "bze.tokenfactory.Query",
	"rewards":        "bze.rewards.Query",
	"burner":         "bze.burner.Query",
	"cointrunk":      "bze.cointrunk.Query",
	"txfeecollector": "bze.txfeecollector.Query",
}

// GRPC is a stand-in for the node's gRPC server: a real grpc.Server on a free
// local port. Every method of the services in Services answers from a
// recorded REST gateway response (the gateway's JSON is the SDK's proto
// JSON), unmarshalled with the chain codec:
//
//	testdata/grpc/<service>/<Method>.<key>.json   when the request has a key
//	testdata/grpc/<service>/<Method>.json         otherwise, or as the fallback
//
// The key of a request is the values of its non-empty top-level string
// fields, in field order, path-escaped and joined by "." (Validator's
// validator_addr; Delegation's delegator_addr and validator_addr). A file
// holding a gateway error ({"code":5,"message":"…","details":[]}) answers
// that gRPC status. A method without a file answers codes.Unimplemented, as a
// node does for a service it does not register. Recorded by
// scripts/record-grpc-fixtures.sh.
type GRPC struct {
	// Addr is the server's host:port.
	Addr string

	codec    *chain.Codec
	fixtures fs.FS
	server   *grpc.Server

	mu        sync.Mutex
	counts    map[string]int
	overrides map[string][]byte
}

// NewGRPC starts a fake gRPC server and stops it when the test ends.
func NewGRPC(t testing.TB) *GRPC {
	t.Helper()
	fixtures, err := fs.Sub(testdata, "testdata/grpc")
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	codec, err := sharedCodec()
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	g := &GRPC{
		Addr:      lis.Addr().String(),
		codec:     codec,
		fixtures:  fixtures,
		counts:    map[string]int{},
		overrides: map[string][]byte{},
	}
	g.server = grpc.NewServer(grpc.ForceServerCodec(rawCodec{}), grpc.UnknownServiceHandler(g.handle))
	go func() { _ = g.server.Serve(lis) }()
	t.Cleanup(g.Stop)
	return g
}

// Stop stops the server: every later call fails as against a node that is
// down. Stopping twice is harmless.
func (g *GRPC) Stop() {
	g.server.Stop()
}

// Requests returns how many calls method of service (a key of Services)
// received, whatever their key.
func (g *GRPC) Requests(service, method string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.counts[service+"/"+method]
}

// RequestsFor returns how many calls method of service received with key.
func (g *GRPC) RequestsFor(service, method, key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.counts[service+"/"+method+"."+key]
}

// ResetRequests sets every counter back to zero.
func (g *GRPC) ResetRequests() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counts = map[string]int{}
}

// SetResponse replaces the recorded answer of method of service for key (""
// for the keyless file) with body, proto JSON or a gateway error. A nil body
// removes the answer.
func (g *GRPC) SetResponse(service, method, key string, body []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if body == nil {
		body = []byte{}
	}
	g.overrides[fixtureName(service, method, key)] = body
}

// ReadGRPCFixture returns the recorded answer of method of service for key
// ("" for the keyless file).
func ReadGRPCFixture(t testing.TB, service, method, key string) []byte {
	t.Helper()
	body, err := fs.ReadFile(testdata, path.Join("testdata/grpc", fixtureName(service, method, key)))
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	return body
}

// LoadGRPCFixture unmarshals the recorded answer of method of service for
// key ("" for the keyless file) into msg with the chain codec, for tests
// that need the recorded data without a server.
func LoadGRPCFixture(t testing.TB, service, method, key string, msg gogoproto.Message) {
	t.Helper()
	codec, err := sharedCodec()
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	if err := codec.ParseProtoJSON(ReadGRPCFixture(t, service, method, key), msg); err != nil {
		t.Fatalf("fakenode: %s/%s %s: %v", service, method, key, err)
	}
}

// sharedCodec builds the chain codec once: it is immutable and costly to
// build.
var sharedCodec = sync.OnceValues(chain.NewCodec)

func fixtureName(service, method, key string) string {
	if key == "" {
		return path.Join(service, method+".json")
	}
	return path.Join(service, method+"."+key+".json")
}

// handle answers every call. The full method is /<service>/<Method>.
func (g *GRPC) handle(_ any, stream grpc.ServerStream) error {
	full, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "no method")
	}
	svcName, method, _ := strings.Cut(strings.TrimPrefix(full, "/"), "/")
	short := ""
	for s, name := range Services {
		if name == svcName {
			short = s
		}
	}
	if short == "" {
		return status.Errorf(codes.Unimplemented, "unknown service %s", svcName)
	}
	md, err := methodDescriptor(svcName, method)
	if err != nil {
		return status.Error(codes.Unimplemented, err.Error())
	}

	var in []byte
	if err := stream.RecvMsg(&in); err != nil {
		return err
	}
	req, err := newMessage(md.Input())
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if err := gogoproto.Unmarshal(in, req); err != nil {
		return status.Errorf(codes.InvalidArgument, "request: %v", err)
	}
	key, err := g.requestKey(md.Input(), req)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}

	g.mu.Lock()
	g.counts[short+"/"+method]++
	if key != "" {
		g.counts[short+"/"+method+"."+key]++
	}
	g.mu.Unlock()

	body, ok := g.answer(short, method, key)
	if !ok {
		return status.Errorf(codes.Unimplemented, "fakenode: no fixture for %s/%s %q", short, method, key)
	}
	if st, ok := gatewayError(body); ok {
		return st.Err()
	}
	resp, err := newMessage(md.Output())
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if err := g.codec.ParseProtoJSON(body, resp); err != nil {
		return status.Errorf(codes.Internal, "fakenode: %s/%s %q: %v", short, method, key, err)
	}
	out, err := gogoproto.Marshal(resp)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return stream.SendMsg(&out)
}

// answer returns the body for key, falling back to the keyless file.
func (g *GRPC) answer(short, method, key string) ([]byte, bool) {
	names := []string{fixtureName(short, method, "")}
	if key != "" {
		names = append([]string{fixtureName(short, method, key)}, names...)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, name := range names {
		if body, ok := g.overrides[name]; ok {
			if len(body) == 0 {
				continue
			}
			return body, true
		}
		if body, err := fs.ReadFile(g.fixtures, name); err == nil {
			return body, true
		}
	}
	return nil, false
}

// requestKey joins the request's non-empty top-level string fields.
func (g *GRPC) requestKey(md protoreflect.MessageDescriptor, req gogoproto.Message) (string, error) {
	raw, err := g.codec.ProtoJSON(req)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	var parts []string
	for i := range md.Fields().Len() {
		fd := md.Fields().Get(i)
		if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
			continue
		}
		var v string
		if b, ok := fields[string(fd.Name())]; ok && json.Unmarshal(b, &v) == nil && v != "" {
			parts = append(parts, url.PathEscape(v))
		}
	}
	return strings.Join(parts, "."), nil
}

func methodDescriptor(service, method string) (protoreflect.MethodDescriptor, error) {
	d, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("service %s: %w", service, err)
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a service", service)
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, fmt.Errorf("service %s has no method %s", service, method)
	}
	return md, nil
}

// newMessage returns a new gogoproto message of the descriptor's type.
func newMessage(d protoreflect.MessageDescriptor) (gogoproto.Message, error) {
	t := gogoproto.MessageType(string(d.FullName()))
	if t == nil {
		return nil, fmt.Errorf("message %s is not registered", d.FullName())
	}
	m, ok := reflect.New(t.Elem()).Interface().(gogoproto.Message)
	if !ok {
		return nil, fmt.Errorf("message %s is not a proto message", d.FullName())
	}
	return m, nil
}

// gatewayError reads a recorded REST gateway error.
func gatewayError(body []byte) (*status.Status, bool) {
	var e struct {
		Code    *int            `json:"code"`
		Message *string         `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil || e.Code == nil || e.Message == nil || *e.Code == 0 {
		return nil, false
	}
	return status.New(codes.Code(*e.Code), *e.Message), true //nolint:gosec // gRPC codes are small
}

// rawCodec passes the frames through: the handler decodes them itself,
// knowing the method only once the call arrived.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, errors.New("rawCodec: not *[]byte")
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return errors.New("rawCodec: not *[]byte")
	}
	*b = append((*b)[:0], data...)
	return nil
}

func (rawCodec) Name() string { return "proto" }
