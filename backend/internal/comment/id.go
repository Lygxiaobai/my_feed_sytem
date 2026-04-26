package comment

import (
	"sync/atomic"
	"time"
)

const (
	maxJSSafeInteger        uint64 = 9007199254740991
	commentIDLookupWindowJS uint64 = 1024
)

var lastGeneratedCommentID atomic.Uint64

func nextCommentID() uint64 {
	for {
		candidate := uint64(time.Now().UnixMicro())
		last := lastGeneratedCommentID.Load()
		if candidate <= last {
			candidate = last + 1
		}
		if lastGeneratedCommentID.CompareAndSwap(last, candidate) {
			return candidate
		}
	}
}

func commentIDLookupRange(id uint64) (uint64, uint64) {
	lower := id
	if lower > commentIDLookupWindowJS {
		lower -= commentIDLookupWindowJS
	} else {
		lower = 0
	}

	upper := id
	if upper > ^uint64(0)-commentIDLookupWindowJS {
		upper = ^uint64(0)
	} else {
		upper += commentIDLookupWindowJS
	}

	return lower, upper
}

func uint64Distance(a uint64, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
