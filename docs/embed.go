// Package topics holds the HAL topics (docs/topics, ADR 0006): the files the
// lesson is grounded on, their parser, and the resolver that decides which copy
// of them to read.
//
// The package lives in docs/ and not under internal/ because //go:embed cannot
// reach a parent directory: the embed has to sit next to docs/topics.
package topics

import "embed"

// FS is the copy of docs/topics baked into the binary. Rooted at "topics", so a
// file is "topics/vault/jwt.md". Use Embedded for the view rooted at the topics
// directory, which is the shape Load and Resolve work with.
//
//go:embed topics
var FS embed.FS
