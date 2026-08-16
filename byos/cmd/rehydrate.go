package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/torrin-app/torrin/byos/internal/mirror"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/manifest"
)

func (d *deps) rehydrate(ctx context.Context, e events.RehydrateBYOS) {
	if e.Node != d.nodeID || d.rc == nil {
		return
	}
	if has, _ := d.src.Has(ctx, manifest.Path(e.InfoHash)); has {
		return
	}
	obj, ok := d.repo.GetBYOSObjectByUserHash(ctx, e.UserID, e.InfoHash)
	if !ok {
		return
	}
	creds, err := d.users.GetStorageCreds(ctx, e.UserID)
	if err != nil || creds == nil || !creds.Enabled || !creds.IsRclone() {
		return
	}

	rctx, cancel := context.WithTimeout(ctx, d.mirrorTimeout)
	defer cancel()
	if err := mirror.Rehydrate(rctx, d.rc, d.src, d.cipher, obj, creds, d.stallTimeout); err != nil {
		slog.Warn("byos: rehydrate failed", "hash", e.InfoHash, "user", e.UserID, "err", err)
		return
	}

	mfFiles := make([]manifest.File, len(obj.Files))
	for i, f := range obj.Files {
		d.repo.AddBlobRef(ctx, e.InfoHash, i, strings.TrimPrefix(f.Key, "blobs/"), f.Size, f.Enc)
		mfFiles[i] = manifest.File{FileName: f.Name, DirectURL: f.Key, FileSize: f.Size, Enc: f.Enc, MediaInfo: f.MediaInfo}
	}
	data, err := manifest.Manifest{InfoHash: e.InfoHash, Name: obj.Name, Files: mfFiles, CreatedAt: time.Now()}.Marshal()
	if err != nil {
		return
	}
	d.src.Put(ctx, manifest.Path(e.InfoHash), bytes.NewReader(data), "application/json")
	slog.Info("byos: rehydrated from storage", "hash", e.InfoHash, "user", e.UserID, "files", len(obj.Files))
}
