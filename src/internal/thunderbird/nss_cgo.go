package thunderbird

// #cgo LDFLAGS: -ldl
// #include <dlfcn.h>
// #include <stdlib.h>
// #include <string.h>
//
// typedef struct { unsigned int type_; void *data; unsigned int len; } mbx_SECItem;
//
// typedef int (*mbx_init_fn)(const char *, const char *, const char *, const char *, unsigned int);
// typedef int (*mbx_decrypt_fn)(mbx_SECItem *, mbx_SECItem *, void *);
// typedef int (*mbx_shutdown_fn)(void);
//
// static int mbx_call_init(void *f, const char *a) { return ((mbx_init_fn)f)(a, "", "", "secmod.db", 1); }
// static int mbx_call_decrypt(void *f, mbx_SECItem *in, mbx_SECItem *out) { return ((mbx_decrypt_fn)f)(in, out, NULL); }
// static int mbx_call_shutdown(void *f) { return ((mbx_shutdown_fn)f)(); }
import "C"

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

const cgoEnabled = "cgo"

// ponytail: NSS decrypt via dlopen(libnss3), same flow as the python ctypes
// version. Needs gcc + libnss3 at runtime; if that ever hurts, fall back to
// MAILBOX_PASSWORD only.
func decryptNSS(profile, blob string) (string, bool) {
	lib := C.CString("libnss3.so")
	defer C.free(unsafe.Pointer(lib))
	h := C.dlopen(lib, C.RTLD_NOW)
	if h == nil {
		return "", false
	}
	defer C.dlclose(h)

	sym := func(name string) unsafe.Pointer {
		n := C.CString(name)
		defer C.free(unsafe.Pointer(n))
		return C.dlsym(h, n)
	}
	initFn := sym("NSS_Initialize")
	decryptFn := sym("PK11SDR_Decrypt")
	shutdownFn := sym("NSS_Shutdown")
	if initFn == nil || decryptFn == nil || shutdownFn == nil {
		return "", false
	}

	tmp, err := os.MkdirTemp("", "mailbox-nss-")
	if err != nil {
		return "", false
	}
	defer os.RemoveAll(tmp)
	for _, name := range []string{"key4.db", "cert9.db", "pkcs11.txt"} {
		src := filepath.Join(profile, name)
		if data, err := os.ReadFile(src); err == nil {
			os.WriteFile(filepath.Join(tmp, name), data, 0o600)
		}
	}

	configStr := C.CString("sql:" + tmp)
	defer C.free(unsafe.Pointer(configStr))
	if rc := C.mbx_call_init(initFn, configStr); rc != 0 {
		return "", false
	}
	defer C.mbx_call_shutdown(shutdownFn)

	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", false
	}
	inData := C.CBytes(raw)
	defer C.free(inData)
	in := C.mbx_SECItem{type_: 0, data: inData, len: C.uint(len(raw))}
	out := C.mbx_SECItem{}
	if C.mbx_call_decrypt(decryptFn, &in, &out) != 0 {
		return "", false
	}
	if out.data == nil || out.len == 0 {
		return "", false
	}
	pw := C.GoBytes(out.data, C.int(out.len))
	// NSS allocates out.data in its own arena (PORTArena / PR_Malloc); free via libc is
	// what the ctypes version implicitly leaks too — small, one-shot process, acceptable.
	return string(pw), true
}

var _ = fmt.Sprintf
