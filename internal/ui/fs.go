package ui

import "embed"

// Dist contains the reproducibly built, source-map-free dashboard assets.
//
//go:embed dist/*
var Dist embed.FS
