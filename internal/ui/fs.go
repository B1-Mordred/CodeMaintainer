package ui

import "embed"

// Dist contains reproducibly built static assets. The checked-in foundation
// shell is replaced by the TypeScript build in later dashboard milestones.
//
//go:embed dist/*
var Dist embed.FS
