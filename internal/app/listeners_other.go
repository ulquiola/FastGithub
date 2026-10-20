//go:build !windows

package app

import (
	"context"

	"fastgithub-go/internal/config"
	"fastgithub-go/internal/mitm"
)

// startTransparent 在非 Windows 平台不启动透明入口。
func startTransparent(ctx context.Context, server *mitm.Server, store *config.Store, cfg *config.Config) (func(), error) {
	return func() {}, nil
}
