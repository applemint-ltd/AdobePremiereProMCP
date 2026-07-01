// Package assets embeds static resources the orchestrator needs at runtime,
// so they ship inside the binary regardless of where it's deployed.
package assets

import _ "embed"

//go:embed render_text_layer.swift
var RenderTextLayerSwift []byte
