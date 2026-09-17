package auth

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>

// The three keychain operations gophermind-osx needs, each taking plain
// C strings/bytes and returning an OSStatus, so the Go side never touches
// CoreFoundation types directly.

static CFMutableDictionaryRef baseQuery(const char *service, const char *account) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFStringRef svc = CFStringCreateWithCString(kCFAllocatorDefault, service, kCFStringEncodingUTF8);
	CFStringRef acc = CFStringCreateWithCString(kCFAllocatorDefault, account, kCFStringEncodingUTF8);
	CFDictionarySetValue(q, kSecAttrService, svc);
	CFDictionarySetValue(q, kSecAttrAccount, acc);
	CFRelease(svc);
	CFRelease(acc);
	return q;
}

static OSStatus keychainSet(const char *service, const char *account, const unsigned char *data, int dataLen) {
	CFMutableDictionaryRef query = baseQuery(service, account);
	CFDataRef cfData = CFDataCreate(kCFAllocatorDefault, data, dataLen);

	CFMutableDictionaryRef attrs = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(attrs, kSecValueData, cfData);
	// Accessible only while the device is unlocked, and never syncs to
	// iCloud Keychain -- a refresh token is a long-lived bearer credential,
	// not something to replicate off this machine.
	CFDictionarySetValue(attrs, kSecAttrAccessible, kSecAttrAccessibleWhenUnlockedThisDeviceOnly);

	OSStatus status = SecItemUpdate(query, attrs);
	if (status == errSecItemNotFound) {
		CFMutableDictionaryRef addQuery = baseQuery(service, account);
		CFDictionarySetValue(addQuery, kSecValueData, cfData);
		CFDictionarySetValue(addQuery, kSecAttrAccessible, kSecAttrAccessibleWhenUnlockedThisDeviceOnly);
		status = SecItemAdd(addQuery, NULL);
		CFRelease(addQuery);
	}

	CFRelease(query);
	CFRelease(attrs);
	CFRelease(cfData);
	return status;
}

// keychainGet writes the found data's length to *outLen and returns a
// malloc'd buffer the Go side must free (via C.free), or NULL with a
// non-zero OSStatus if not found/on error.
static unsigned char *keychainGet(const char *service, const char *account, int *outLen, OSStatus *outStatus) {
	CFMutableDictionaryRef query = baseQuery(service, account);
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);

	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	CFRelease(query);
	*outStatus = status;
	if (status != errSecSuccess || result == NULL) {
		return NULL;
	}

	CFDataRef data = (CFDataRef)result;
	CFIndex len = CFDataGetLength(data);
	unsigned char *buf = malloc(len);
	CFDataGetBytes(data, CFRangeMake(0, len), buf);
	*outLen = (int)len;
	CFRelease(result);
	return buf;
}

static OSStatus keychainDelete(const char *service, const char *account) {
	CFMutableDictionaryRef query = baseQuery(service, account);
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// ErrNotFound is returned by TokenStore.Load when no entry exists for a
// backend. Exported so callers (GetToken) can errors.Is it to distinguish
// "never logged in" from a real storage failure.
var ErrNotFound = errors.New("no credentials stored for this backend")

// keychainService namespaces every entry this package writes, so it never
// collides with an unrelated app's Keychain items sharing an account name.
const keychainService = "com.gophermind.gophermind-osx"

// keychainStore is the real, macOS-Keychain-backed TokenStore.
type keychainStore struct{}

// NewKeychainStore returns the real, macOS-Keychain-backed TokenStore,
// exported so callers outside this package can use it directly -- both
// for NewManager's OAuth token storage and, keyed via BackendTokenKey,
// for a configured backend's plain bearer token. Before this existed,
// keychainStore{} could only be constructed from within this package
// (NewManager's own doc comment says to "pass keychainStore{} for real
// use," which no external caller could actually do).
func NewKeychainStore() TokenStore { return keychainStore{} }

// BackendTokenKey namespaces a configured backend's plain bearer token
// distinctly from its OAuth Token blob in the same TokenStore: Manager's
// own Save/Load key by the bare backend name, so storing a bearer token
// under that same key would silently collide with (and could overwrite,
// or be overwritten by) that backend's OAuth tokens.
func BackendTokenKey(name string) string {
	return "bearer:" + name
}

// errSecSuccess/errSecItemNotFound are OSStatus values from
// <Security/SecBase.h>, duplicated here (rather than referenced via cgo,
// which is straightforward for the C side but awkward to compare a C.OSStatus
// constant against on the Go side across CGO_ENABLED build variations) as
// plain int32 constants -- these two specific values are part of the stable
// public Keychain API and have not changed since Mac OS X 10.6.
const (
	errSecSuccess      = 0
	errSecItemNotFound = -25300
)

func (keychainStore) Save(backend string, data []byte) error {
	cService := C.CString(keychainService)
	cAccount := C.CString(backend)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))

	var cData *C.uchar
	if len(data) > 0 {
		cData = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	status := C.keychainSet(cService, cAccount, cData, C.int(len(data)))
	if status != errSecSuccess {
		return fmt.Errorf("keychain: save failed (OSStatus %d)", status)
	}
	return nil
}

func (keychainStore) Load(backend string) ([]byte, error) {
	cService := C.CString(keychainService)
	cAccount := C.CString(backend)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))

	var outLen C.int
	var outStatus C.OSStatus
	buf := C.keychainGet(cService, cAccount, &outLen, &outStatus)
	if buf == nil {
		if int32(outStatus) == errSecItemNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("keychain: load failed (OSStatus %d)", outStatus)
	}
	defer C.free(unsafe.Pointer(buf))
	return C.GoBytes(unsafe.Pointer(buf), outLen), nil
}

func (keychainStore) Delete(backend string) error {
	cService := C.CString(keychainService)
	cAccount := C.CString(backend)
	defer C.free(unsafe.Pointer(cService))
	defer C.free(unsafe.Pointer(cAccount))

	status := C.keychainDelete(cService, cAccount)
	if status != errSecSuccess && int32(status) != errSecItemNotFound {
		return fmt.Errorf("keychain: delete failed (OSStatus %d)", status)
	}
	return nil
}
