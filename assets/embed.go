package assets

import _ "embed"

//go:embed AppIcon-1024.png
var AppIconPNG []byte

//go:embed AppIcon-256.png
var AppIcon256PNG []byte

//go:embed MenuBarApp.swift
var MacOSMenuBarSource []byte
