package gateway

import (
	"embed"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed debug/*
var debugAssets embed.FS

func handleDebug(w http.ResponseWriter, r *http.Request) {
	assetPath := strings.TrimPrefix(r.URL.Path, "/debug")
	if assetPath == "" || assetPath == "/" {
		serveDebugAsset(w, r, "index.html")
		return
	}
	assetPath = strings.TrimPrefix(assetPath, "/")
	if assetPath == "" || strings.Contains(assetPath, "..") || path.Clean(assetPath) != assetPath {
		http.NotFound(w, r)
		return
	}
	serveDebugAsset(w, r, assetPath)
}

func serveDebugAsset(w http.ResponseWriter, r *http.Request, name string) {
	content, err := debugAssets.ReadFile("debug/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
