package main

//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.5.0 -64 -o resource_windows_amd64.syso -ver-major=$MUSIC_OPS_VERSION_MAJOR -ver-minor=$MUSIC_OPS_VERSION_MINOR -ver-patch=$MUSIC_OPS_VERSION_PATCH -product-ver-major=$MUSIC_OPS_VERSION_MAJOR -product-ver-minor=$MUSIC_OPS_VERSION_MINOR -product-ver-patch=$MUSIC_OPS_VERSION_PATCH -file-version=$MUSIC_OPS_VERSION -product-version=$MUSIC_OPS_VERSION versioninfo.json
