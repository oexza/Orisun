// Command orisun-ffi builds Orisun's desktop C ABI as a shared library.
package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"time"
	"unsafe"

	desktop "github.com/OrisunLabs/Orisun/embedded/sqlite/desktop"
)

var bridge = desktop.NewBridge()

//export orisun_abi_version
func orisun_abi_version() C.uint32_t {
	return C.uint32_t(desktop.ABIVersion)
}

//export orisun_open
func orisun_open(dataDirectory, boundariesJSON *C.char) *C.char {
	return C.CString(bridge.Open(goString(dataDirectory), goString(boundariesJSON)))
}

//export orisun_close
func orisun_close(storeHandle C.uint64_t) *C.char {
	return C.CString(bridge.Close(uint64(storeHandle)))
}

//export orisun_save_events
func orisun_save_events(storeHandle C.uint64_t, boundary, eventsJSON, expectedPositionJSON, queryJSON *C.char) *C.char {
	return C.CString(bridge.SaveEvents(
		uint64(storeHandle),
		goString(boundary),
		goString(eventsJSON),
		goString(expectedPositionJSON),
		goString(queryJSON),
	))
}

//export orisun_get_events
func orisun_get_events(storeHandle C.uint64_t, boundary, fromPositionJSON, queryJSON *C.char, count C.int64_t, descending C.int32_t) *C.char {
	return C.CString(bridge.GetEvents(
		uint64(storeHandle),
		goString(boundary),
		goString(fromPositionJSON),
		goString(queryJSON),
		int(count),
		descending != 0,
	))
}

//export orisun_get_latest_by_criteria
func orisun_get_latest_by_criteria(storeHandle C.uint64_t, boundary, queryJSON *C.char) *C.char {
	return C.CString(bridge.GetLatestByCriteria(uint64(storeHandle), goString(boundary), goString(queryJSON)))
}

//export orisun_create_boundary_index
func orisun_create_boundary_index(storeHandle C.uint64_t, boundary, name, fieldsJSON, conditionsJSON, combinator *C.char) *C.char {
	return C.CString(bridge.CreateBoundaryIndex(
		uint64(storeHandle),
		goString(boundary),
		goString(name),
		goString(fieldsJSON),
		goString(conditionsJSON),
		goString(combinator),
	))
}

//export orisun_drop_boundary_index
func orisun_drop_boundary_index(storeHandle C.uint64_t, boundary, name *C.char) *C.char {
	return C.CString(bridge.DropBoundaryIndex(uint64(storeHandle), goString(boundary), goString(name)))
}

//export orisun_subscribe
func orisun_subscribe(storeHandle C.uint64_t, boundary, subscriberName, afterPositionJSON, queryJSON *C.char) *C.char {
	return C.CString(bridge.Subscribe(
		uint64(storeHandle),
		goString(boundary),
		goString(subscriberName),
		goString(afterPositionJSON),
		goString(queryJSON),
	))
}

//export orisun_subscription_next
func orisun_subscription_next(subscriptionHandle C.uint64_t, timeoutMilliseconds C.int64_t) *C.char {
	timeout := time.Duration(timeoutMilliseconds) * time.Millisecond
	return C.CString(bridge.NextSubscriptionMessage(uint64(subscriptionHandle), timeout))
}

//export orisun_subscription_stop
func orisun_subscription_stop(subscriptionHandle C.uint64_t) *C.char {
	return C.CString(bridge.StopSubscription(uint64(subscriptionHandle)))
}

//export orisun_free_string
func orisun_free_string(value *C.char) {
	C.free(unsafe.Pointer(value))
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func main() {}
