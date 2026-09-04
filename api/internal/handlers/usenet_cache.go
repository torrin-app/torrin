package handlers

import "github.com/torrin-app/torrin/shared/keyed"

func lockGrab(hash string) func() { return keyed.Lock(hash) }
