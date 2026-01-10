//go:build cgo
// +build cgo

package sqlite

/*
#cgo CFLAGS: -DSQLITE_MAX_VARIABLE_NUMBER=250000
#cgo LDFLAGS: -lm
*/
import "C"
