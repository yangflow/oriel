//go:build !darwin

package main

// Other systems do not display Dock badges, so the controller never requests this permission.
func requestDockBadge(done func(bool)) { done(false) }
