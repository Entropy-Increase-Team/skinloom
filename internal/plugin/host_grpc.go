package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	"github.com/Entropy-Increase-Team/skinloom/ent"
)

// hostGRPCServer implements pluginv1.HostServiceServer on the host side over
// hashicorp/go-plugin's GRPCBroker so Tier 2 native plugins invoke the exact
// same permission-checked domain services as Tier 1 JS plugins.
type hostGRPCServer struct {
	pluginv1.UnimplementedHostServiceServer
	rt *pluginRuntime
}

func newHostGRPCServer(rt *pluginRuntime) *hostGRPCServer {
	return &hostGRPCServer{rt: rt}
}

func mapHostErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPermissionDenied) {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	if ent.IsNotFound(err) || errors.Is(err, ErrFileNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, ErrQuotaExceeded) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func decodeOpArgs(req *pluginv1.HostOpRequest) ([]any, error) {
	args := []any{req.GetOp()}
	if len(req.GetArgsJson()) > 0 {
		var extra []any
		if err := json.Unmarshal(req.GetArgsJson(), &extra); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid args_json: %v", err)
		}
		args = append(args, extra...)
	}
	return args, nil
}

func encodeOpResp(val any) (*pluginv1.HostOpResponse, error) {
	if b, ok := val.([]byte); ok {
		return &pluginv1.HostOpResponse{BinaryData: b}, nil
	}
	if val == nil {
		return &pluginv1.HostOpResponse{}, nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal host response: %v", err)
	}
	return &pluginv1.HostOpResponse{ResultJson: raw}, nil
}

func (s *hostGRPCServer) dispatchOp(
	ctx context.Context,
	req *pluginv1.HostOpRequest,
	fn func(context.Context, []any) (any, error),
) (*pluginv1.HostOpResponse, error) {
	if req.GetLocale() != "" {
		ctx = WithRequestLocale(ctx, req.GetLocale())
	}
	args, err := decodeOpArgs(req)
	if err != nil {
		return nil, err
	}
	res, err := fn(ctx, args)
	if err != nil {
		return nil, mapHostErr(err)
	}
	return encodeOpResp(res)
}

func (s *hostGRPCServer) KV(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostKV)
}

func (s *hostGRPCServer) Docs(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostDB)
}

func (s *hostGRPCServer) FilesPut(ctx context.Context, req *pluginv1.FilesPutRequest) (*pluginv1.FilesPutResponse, error) {
	u, err := s.rt.files.Put(ctx, req.GetKey(), req.GetData())
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.FilesPutResponse{Url: u}, nil
}

func (s *hostGRPCServer) FilesGet(ctx context.Context, req *pluginv1.FilesKeyRequest) (*pluginv1.FilesGetResponse, error) {
	data, err := s.rt.files.Get(ctx, req.GetKey())
	if errors.Is(err, ErrFileNotFound) {
		return &pluginv1.FilesGetResponse{Found: false}, nil
	}
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.FilesGetResponse{Found: true, Data: data}, nil
}

func (s *hostGRPCServer) FilesDelete(ctx context.Context, req *pluginv1.FilesKeyRequest) (*pluginv1.FilesDeleteResponse, error) {
	deleted, err := s.rt.files.Delete(ctx, req.GetKey())
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.FilesDeleteResponse{Deleted: deleted}, nil
}

func (s *hostGRPCServer) FilesURL(_ context.Context, req *pluginv1.FilesKeyRequest) (*pluginv1.FilesURLResponse, error) {
	u, err := s.rt.files.URL(req.GetKey())
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.FilesURLResponse{Url: u}, nil
}

func (s *hostGRPCServer) Fetch(ctx context.Context, req *pluginv1.FetchRequest) (*pluginv1.FetchResponse, error) {
	resp, err := s.rt.fetch.Fetch(ctx, req.GetUrl(), FetchRequest{
		Method:  req.GetMethod(),
		Headers: req.GetHeaders(),
		Body:    string(req.GetBody()),
		Timeout: int(req.GetTimeoutMs()),
	})
	if err != nil {
		return nil, mapHostErr(err)
	}
	bodyBytes := []byte(resp.BodyText)
	if resp.BodyBase64 != "" {
		if decoded, decErr := base64.StdEncoding.DecodeString(resp.BodyBase64); decErr == nil {
			bodyBytes = decoded
		}
	}
	return &pluginv1.FetchResponse{
		Ok:         resp.OK,
		Status:     int32(resp.Status),
		StatusText: resp.StatusText,
		Headers:    resp.Headers,
		Url:        resp.URL,
		Body:       bodyBytes,
	}, nil
}

func (s *hostGRPCServer) Settings(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostSettings)
}

func (s *hostGRPCServer) Site(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostSite)
}

func (s *hostGRPCServer) Users(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostUsers)
}

func (s *hostGRPCServer) Players(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostPlayers)
}

func (s *hostGRPCServer) Textures(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	if req.GetLocale() != "" {
		ctx = WithRequestLocale(ctx, req.GetLocale())
	}
	args, err := decodeOpArgs(req)
	if err != nil {
		return nil, err
	}
	// If binary_data is supplied for textures.create, attach it directly to the input map.
	if req.GetOp() == "create" && len(req.GetBinaryData()) > 0 && len(args) > 1 {
		if m, ok := args[1].(map[string]any); ok {
			m["data"] = req.GetBinaryData()
		}
	}
	res, err := s.rt.hostTextures(ctx, args)
	if err != nil {
		return nil, mapHostErr(err)
	}
	return encodeOpResp(res)
}

func (s *hostGRPCServer) Closet(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostCloset)
}

func (s *hostGRPCServer) Score(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostScore)
}

func (s *hostGRPCServer) Notify(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostNotify)
}

func (s *hostGRPCServer) Mail(ctx context.Context, req *pluginv1.MailSendRequest) (*pluginv1.HostOpResponse, error) {
	if req.GetLocale() != "" {
		ctx = WithRequestLocale(ctx, req.GetLocale())
	}
	var spec map[string]any
	if len(req.GetOptionsJson()) > 0 {
		if err := json.Unmarshal(req.GetOptionsJson(), &spec); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid options_json: %v", err)
		}
	}
	res, err := s.rt.hostMail(ctx, []any{spec})
	if err != nil {
		return nil, mapHostErr(err)
	}
	return encodeOpResp(res)
}

func (s *hostGRPCServer) Yggdrasil(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	// Native plugins only invoke query ops (publicKey, lookupName, lookupProfile) via HostService;
	// metadata extensions are declared in InitResponse.
	switch req.GetOp() {
	case "publicKey", "lookupName", "lookupProfile":
		return s.dispatchOp(ctx, req, s.rt.hostYggdrasil)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported yggdrasil host op %q", req.GetOp())
	}
}

func (s *hostGRPCServer) Audit(ctx context.Context, req *pluginv1.AuditRecordRequest) (*pluginv1.Empty, error) {
	var meta map[string]any
	if len(req.GetMetaJson()) > 0 {
		if err := json.Unmarshal(req.GetMetaJson(), &meta); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid meta_json: %v", err)
		}
	}
	_, err := s.rt.hostAudit(ctx, []any{req.GetAction(), req.GetTargetType(), req.GetTargetId(), meta})
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.Empty{}, nil
}

func (s *hostGRPCServer) EmitEvent(ctx context.Context, req *pluginv1.EmitEventRequest) (*pluginv1.Empty, error) {
	var payload any
	if len(req.GetPayloadJson()) > 0 {
		if err := json.Unmarshal(req.GetPayloadJson(), &payload); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid payload_json: %v", err)
		}
	}
	_, err := s.rt.hostEmitEvent(ctx, []any{req.GetLocalName(), payload})
	if err != nil {
		return nil, mapHostErr(err)
	}
	return &pluginv1.Empty{}, nil
}

func (s *hostGRPCServer) CallPluginService(ctx context.Context, req *pluginv1.CallPluginServiceRequest) (*pluginv1.HostOpResponse, error) {
	var callArgs []any
	if len(req.GetArgsJson()) > 0 {
		if err := json.Unmarshal(req.GetArgsJson(), &callArgs); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid args_json: %v", err)
		}
	}
	res, err := s.rt.hostServices(ctx, []any{"call", req.GetTargetPluginId(), req.GetServiceName(), callArgs})
	if err != nil {
		return nil, mapHostErr(err)
	}
	return encodeOpResp(res)
}

func (s *hostGRPCServer) Log(_ context.Context, req *pluginv1.LogRequest) (*pluginv1.Empty, error) {
	s.rt.appendLog(req.GetLevel(), req.GetMessage())
	return &pluginv1.Empty{}, nil
}

func (s *hostGRPCServer) Cache(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostCache)
}

func (s *hostGRPCServer) I18N(ctx context.Context, req *pluginv1.HostOpRequest) (*pluginv1.HostOpResponse, error) {
	return s.dispatchOp(ctx, req, s.rt.hostI18n)
}

func unwrapGRPCError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	if st.Code() == codes.PermissionDenied {
		return fmt.Errorf("%w: %s", ErrPermissionDenied, st.Message())
	}
	return errors.New(st.Message())
}
