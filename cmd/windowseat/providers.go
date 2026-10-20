package main

import "windowseat/internal/providers"

// newReaders is the single place where provider adapters are wired in.
// Add one reader per provider here.
func newReaders() []providers.Reader {
	return nil
}
