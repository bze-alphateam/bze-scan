package chain

import (
	"encoding/json"
	"fmt"
	"slices"

	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/encoding"

	bzeapp "github.com/bze-alphateam/bze/app"
)

// Tx is a decoded transaction, reduced to what the explorer stores.
type Tx struct {
	Memo string
	// Fee is AuthInfo.Fee.Amount.
	Fee sdk.Coins
	// FeePayer is the account the fee was deducted from: the granter when a
	// fee grant paid, else the payer (the first signer unless set).
	FeePayer string
	// Signers are the signer addresses in order; empty when a message could
	// not be decoded.
	Signers []string
	Msgs    []Msg
}

// Msg is one message of a transaction.
type Msg struct {
	// TypeURL is the current type URL: a pre-v8 BZE message is reported
	// under its current URL (see CanonicalTypeURL).
	TypeURL string
	// Signer is the message's first signer, as the SDK sets message.sender;
	// empty when the message could not be decoded.
	Signer string
	// Body is the message as proto JSON; nil when the type URL is not in the
	// chain's registry.
	Body json.RawMessage
	// Err says why Body is nil.
	Err error
}

// Codec decodes BZE transactions with the chain's own interface registry:
// every module the chain wires through depinject, plus the IBC modules the
// chain registers by hand.
type Codec struct {
	cdc      codec.Codec
	registry codectypes.InterfaceRegistry
	txConfig client.TxConfig
}

// NewCodec builds the codec from the chain's app configuration without
// instantiating the app.
func NewCodec() (*Codec, error) {
	var (
		cdc      codec.Codec
		registry codectypes.InterfaceRegistry
	)
	if err := depinject.Inject(
		depinject.Configs(bzeapp.AppConfig(), depinject.Supply(log.NewNopLogger())),
		&cdc, &registry,
	); err != nil {
		return nil, fmt.Errorf("chain codec: %w", err)
	}
	bzeapp.RegisterIBC(registry)
	if err := registerLegacyTypeURLs(registry); err != nil {
		return nil, fmt.Errorf("chain codec: %w", err)
	}

	txConfig, err := authtx.NewTxConfigWithOptions(cdc, authtx.ConfigOptions{
		EnabledSignModes: []signingtypes.SignMode{signingtypes.SignMode_SIGN_MODE_DIRECT},
	})
	if err != nil {
		return nil, fmt.Errorf("chain codec: tx config: %w", err)
	}
	return &Codec{cdc: cdc, registry: registry, txConfig: txConfig}, nil
}

// DecodeTx decodes the raw bytes of a transaction. It fails when any message
// has a type URL the registry does not know.
func (c *Codec) DecodeTx(raw []byte) (sdk.Tx, error) {
	return c.txConfig.TxDecoder()(raw)
}

// MsgJSON renders a message as proto JSON. Nested Any values (the messages of
// an authz MsgExec, for instance) carry their "@type", the current one for a
// pre-v8 BZE message (see CanonicalTypeURL).
func (c *Codec) MsgJSON(msg sdk.Msg) ([]byte, error) {
	out, err := c.cdc.MarshalJSON(msg)
	if err != nil {
		return nil, err
	}
	return canonicalNestedTypes(out), nil
}

// TypeURL is the message's type URL, e.g. "/cosmos.bank.v1beta1.MsgSend".
func TypeURL(msg sdk.Msg) string {
	return sdk.MsgTypeURL(msg)
}

// MsgTypeURLs lists every message type URL registered in the chain's
// interface registry, without the legacy aliases of LegacyTypeURLs.
func (c *Codec) MsgTypeURLs() []string {
	return slices.DeleteFunc(c.registry.ListImplementations(sdk.MsgInterfaceProtoName), func(u string) bool {
		_, legacy := legacyTypeURLs[u]
		return legacy
	})
}

// Decode decodes a transaction into a Tx. A message whose type URL the
// registry does not know does not fail the transaction: it keeps its type URL
// and gets a nil Body. Decode fails only when the bytes are not a transaction.
func (c *Codec) Decode(raw []byte) (*Tx, error) {
	decoded, err := c.DecodeTx(raw)
	if err != nil {
		return c.decodeLenient(raw, err)
	}

	out := &Tx{}
	// The signers of every message, deduplicated in order of first
	// appearance as the SDK does: the fallback for transactions whose own
	// signer lookup fails (pre-v8 type URLs have no descriptor under their
	// old name, only their current Go type has).
	var msgSigners []string
	for _, msg := range decoded.GetMsgs() {
		m, signers := c.msg(msg)
		out.Msgs = append(out.Msgs, m)
		for _, a := range signers {
			if !slices.Contains(msgSigners, a) {
				msgSigners = append(msgSigners, a)
			}
		}
	}
	if m, ok := decoded.(sdk.TxWithMemo); ok {
		out.Memo = m.GetMemo()
	}
	if s, ok := decoded.(authsigning.SigVerifiableTx); ok {
		if signers, err := s.GetSigners(); err == nil {
			for _, a := range signers {
				out.Signers = append(out.Signers, accAddress(a))
			}
		} else {
			out.Signers = msgSigners
		}
	}
	if f, ok := decoded.(sdk.FeeTx); ok {
		out.Fee = f.GetFee()
		payer := f.FeeGranter()
		if len(payer) == 0 {
			payer = f.FeePayer()
		}
		out.FeePayer = accAddress(payer)
		if out.FeePayer == "" && len(out.Signers) > 0 {
			out.FeePayer = out.Signers[0]
		}
	}
	return out, nil
}

// decodeLenient reads the transaction without resolving its messages, then
// resolves each message on its own, so one unknown type URL costs only that
// message's body.
func (c *Codec) decodeLenient(raw []byte, cause error) (*Tx, error) {
	var txRaw tx.TxRaw
	if err := txRaw.Unmarshal(raw); err != nil {
		return nil, fmt.Errorf("not a transaction: %w (decoder: %v)", err, cause)
	}
	var body tx.TxBody
	if err := body.Unmarshal(txRaw.BodyBytes); err != nil {
		return nil, fmt.Errorf("transaction body: %w (decoder: %v)", err, cause)
	}
	out := &Tx{Memo: body.Memo}
	var auth tx.AuthInfo
	if err := auth.Unmarshal(txRaw.AuthInfoBytes); err == nil && auth.Fee != nil {
		out.Fee = auth.Fee.Amount
		out.FeePayer = auth.Fee.Granter
		if out.FeePayer == "" {
			out.FeePayer = auth.Fee.Payer
		}
	}
	for _, a := range body.Messages {
		var msg sdk.Msg
		if err := c.registry.UnpackAny(a, &msg); err != nil {
			out.Msgs = append(out.Msgs, Msg{TypeURL: CanonicalTypeURL(a.TypeUrl), Err: err})
			continue
		}
		m, _ := c.msg(msg)
		out.Msgs = append(out.Msgs, m)
	}
	return out, nil
}

// msg renders one decoded message and returns its signers.
func (c *Codec) msg(msg sdk.Msg) (Msg, []string) {
	m := Msg{TypeURL: TypeURL(msg)}
	var signers []string
	if addrs, _, err := c.cdc.GetMsgV1Signers(msg); err == nil {
		for _, a := range addrs {
			signers = append(signers, accAddress(a))
		}
	}
	if len(signers) > 0 {
		m.Signer = signers[0]
	}
	var err error
	if m.Body, err = c.MsgJSON(msg); err != nil {
		m.Body, m.Err = nil, err
	}
	return m, signers
}

func accAddress(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s, err := bech32.ConvertAndEncode(Bech32Prefix, b)
	if err != nil {
		return ""
	}
	return s
}

// GRPC is the gRPC codec for the chain's query services: protobuf, with the
// Any values of a message (a validator's consensus key, for instance)
// resolved against the chain's registry.
func (c *Codec) GRPC() encoding.Codec {
	return codec.NewProtoCodec(c.registry).GRPCCodec()
}

// ProtoJSON renders a message as proto JSON, the shape of the REST
// gateway's answers.
func (c *Codec) ProtoJSON(msg gogoproto.Message) ([]byte, error) {
	return c.cdc.MarshalJSON(msg)
}

// ParseProtoJSON reads proto JSON (a REST gateway answer, for instance) into
// msg, resolving "@type" values against the chain's registry.
func (c *Codec) ParseProtoJSON(bz []byte, msg gogoproto.Message) error {
	return c.cdc.UnmarshalJSON(bz, msg)
}

// InterfaceRegistry resolves the Any values of the chain's messages (a
// validator's consensus key, for instance).
func (c *Codec) InterfaceRegistry() codectypes.InterfaceRegistry {
	return c.registry
}
