package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	pluginv1 "github.com/Entropy-Increase-Team/skinloom/api/proto/plugin/v1"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// grpcStorageProvider bridges a Tier 2 native plugin's StorageProviderService
// onto SkinLoom's hot-swappable storage.Store and storage.Linker interfaces.
type grpcStorageProvider struct {
	driver *NativeDriver
}

func (p *grpcStorageProvider) Put(ctx context.Context, key string, data []byte) error {
	if !storage.ValidKey(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	return p.driver.withCall(ctx, defaultRouteTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.storage == nil {
			return errors.New("native plugin does not implement StorageProviderService")
		}
		_, err := h.storage.Put(callCtx, &pluginv1.StoragePutRequest{Key: key, Data: data})
		return err
	})
}

func (p *grpcStorageProvider) Open(ctx context.Context, key string) (storage.File, error) {
	if !storage.ValidKey(key) {
		return nil, storage.ErrNotFound
	}
	var resp *pluginv1.StorageGetResponse
	err := p.driver.withCall(ctx, defaultRouteTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.storage == nil {
			return errors.New("native plugin does not implement StorageProviderService")
		}
		var rpcErr error
		resp, rpcErr = h.storage.Get(callCtx, &pluginv1.StorageKeyRequest{Key: key})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetFound() {
		return nil, storage.ErrNotFound
	}
	mt := time.Unix(0, resp.GetModTimeUnixNano()).UTC()
	return storage.NewBytesFile(resp.GetData(), mt), nil
}

func (p *grpcStorageProvider) Stat(ctx context.Context, key string) (time.Time, error) {
	if !storage.ValidKey(key) {
		return time.Time{}, storage.ErrNotFound
	}
	var resp *pluginv1.StorageStatResponse
	err := p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.storage == nil {
			return errors.New("native plugin does not implement StorageProviderService")
		}
		var rpcErr error
		resp, rpcErr = h.storage.Stat(callCtx, &pluginv1.StorageKeyRequest{Key: key})
		return rpcErr
	})
	if err != nil {
		return time.Time{}, err
	}
	if !resp.GetExists() {
		return time.Time{}, storage.ErrNotFound
	}
	return time.Unix(0, resp.GetModTimeUnixNano()).UTC(), nil
}

func (p *grpcStorageProvider) Walk(ctx context.Context, fn func(key string, modTime time.Time) error) error {
	const maxPages = 10000
	cursor := ""
	seen := map[string]bool{"": true}
	for page := 0; page < maxPages; page++ {
		var resp *pluginv1.StorageListResponse
		err := p.driver.withCall(ctx, defaultRouteTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
			if h.storage == nil {
				return errors.New("native plugin does not implement StorageProviderService")
			}
			var rpcErr error
			resp, rpcErr = h.storage.List(callCtx, &pluginv1.StorageListRequest{Cursor: cursor, Limit: 500})
			return rpcErr
		})
		if err != nil {
			return err
		}
		for _, ent := range resp.GetEntries() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !storage.ValidKey(ent.GetKey()) {
				continue
			}
			if err := fn(ent.GetKey(), time.Unix(0, ent.GetModTimeUnixNano()).UTC()); err != nil {
				return err
			}
		}
		next := resp.GetNextCursor()
		if next == "" || len(resp.GetEntries()) == 0 {
			return nil
		}
		if seen[next] {
			return fmt.Errorf("storage provider returned repeated pagination cursor %q", next)
		}
		seen[next] = true
		cursor = next
	}
	return errors.New("storage provider Walk exceeded maximum page limit")
}

func (p *grpcStorageProvider) Delete(ctx context.Context, key string) error {
	if !storage.ValidKey(key) {
		return nil
	}
	return p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.storage == nil {
			return errors.New("native plugin does not implement StorageProviderService")
		}
		_, err := h.storage.Delete(callCtx, &pluginv1.StorageKeyRequest{Key: key})
		return err
	})
}

func (p *grpcStorageProvider) URL(key string) string {
	if !storage.ValidKey(key) {
		return ""
	}
	var resp *pluginv1.StorageURLResponse
	err := p.driver.withCall(context.Background(), 2*time.Second, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.storage == nil {
			return errors.New("missing storage service")
		}
		var rpcErr error
		resp, rpcErr = h.storage.URL(callCtx, &pluginv1.StorageKeyRequest{Key: key})
		return rpcErr
	})
	if err != nil || resp == nil {
		return ""
	}
	raw := strings.TrimSpace(resp.GetUrl())
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

var (
	_ storage.Store  = (*grpcStorageProvider)(nil)
	_ storage.Linker = (*grpcStorageProvider)(nil)
)

// grpcSearchProvider bridges SearchProviderService onto texture.SearchProvider.
type grpcSearchProvider struct {
	driver *NativeDriver
}

func (p *grpcSearchProvider) Index(ctx context.Context, doc texture.SearchDoc) error {
	return p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.search == nil {
			return errors.New("native plugin does not implement SearchProviderService")
		}
		_, err := h.search.Index(callCtx, &pluginv1.SearchIndexRequest{
			Doc: &pluginv1.TextureSearchDoc{
				Id:            doc.ID,
				Name:          doc.Name,
				Type:          doc.Type,
				Hash:          doc.Hash,
				UploaderId:    doc.UploaderID,
				Public:        doc.Public,
				Likes:         int32(doc.Likes),
				CreatedAtUnix: doc.CreatedAt.Unix(),
				UpdatedAtUnix: doc.UpdatedAt.Unix(),
			},
		})
		return err
	})
}

func (p *grpcSearchProvider) Delete(ctx context.Context, textureID int64) error {
	return p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.search == nil {
			return errors.New("native plugin does not implement SearchProviderService")
		}
		_, err := h.search.Delete(callCtx, &pluginv1.SearchDeleteRequest{TextureId: textureID})
		return err
	})
}

func (p *grpcSearchProvider) Search(ctx context.Context, q texture.SearchQuery) (texture.SearchResult, error) {
	var resp *pluginv1.SearchQueryResponse
	err := p.driver.withCall(ctx, defaultGuardTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.search == nil {
			return errors.New("native plugin does not implement SearchProviderService")
		}
		var rpcErr error
		resp, rpcErr = h.search.Search(callCtx, &pluginv1.SearchQueryRequest{
			Query:          q.Query,
			Category:       q.Category,
			UploaderId:     q.UploaderID,
			IncludePrivate: q.IncludePrivate,
			Sort:           q.Sort,
			Cursor:         q.Cursor,
			Limit:          int32(q.Limit),
		})
		return rpcErr
	})
	if err != nil {
		return texture.SearchResult{}, err
	}
	return texture.SearchResult{
		IDs:        append([]int64(nil), resp.GetTextureIds()...),
		NextCursor: resp.GetNextCursor(),
		Total:      resp.GetTotal(),
	}, nil
}

var _ texture.SearchProvider = (*grpcSearchProvider)(nil)

// grpcRenderProvider bridges RenderProviderService onto render.Provider.
type grpcRenderProvider struct {
	driver *NativeDriver
}

func (p *grpcRenderProvider) RenderAvatar(ctx context.Context, req render.AvatarRequest) ([]byte, error) {
	var resp *pluginv1.RenderImageResponse
	err := p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.render == nil {
			return errors.New("native plugin does not implement RenderProviderService")
		}
		var rpcErr error
		resp, rpcErr = h.render.RenderAvatar(callCtx, &pluginv1.RenderAvatarRequest{
			SkinPng: req.SkinPNG,
			Size:    int32(req.Size),
			Is_3D:   req.Is3D,
			Format:  req.Format,
		})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	return resp.GetImageBytes(), nil
}

func (p *grpcRenderProvider) RenderPreview(ctx context.Context, req render.PreviewRequest) ([]byte, error) {
	var resp *pluginv1.RenderImageResponse
	err := p.driver.withCall(ctx, defaultHookTimeout, func(callCtx context.Context, h *nativeProcessHandle) error {
		if h.render == nil {
			return errors.New("native plugin does not implement RenderProviderService")
		}
		var rpcErr error
		resp, rpcErr = h.render.RenderPreview(callCtx, &pluginv1.RenderPreviewRequest{
			TexturePng:  req.TexturePNG,
			TextureType: req.TextureType,
			Height:      int32(req.Height),
			Format:      req.Format,
		})
		return rpcErr
	})
	if err != nil {
		return nil, err
	}
	return resp.GetImageBytes(), nil
}

var _ render.Provider = (*grpcRenderProvider)(nil)
