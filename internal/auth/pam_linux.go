//go:build linux && cgo

package auth

/*
#cgo LDFLAGS: -lpam
#include <security/pam_appl.h>
#include <stdlib.h>
#include <string.h>

static int monitor_conversation(int num_msg, const struct pam_message **msg,
                                struct pam_response **resp, void *data) {
	if (num_msg <= 0 || num_msg > 16) return PAM_CONV_ERR;
	struct pam_response *replies = calloc((size_t)num_msg, sizeof(struct pam_response));
	if (replies == NULL) return PAM_BUF_ERR;
	const char *password = (const char *)data;
	for (int i = 0; i < num_msg; i++) {
		switch (msg[i]->msg_style) {
		case PAM_PROMPT_ECHO_OFF:
			replies[i].resp = strdup(password);
			if (replies[i].resp == NULL) {
				for (int j = 0; j < i; j++) free(replies[j].resp);
				free(replies);
				return PAM_BUF_ERR;
			}
			break;
		case PAM_PROMPT_ECHO_ON:
			replies[i].resp = strdup("");
			break;
		case PAM_ERROR_MSG:
		case PAM_TEXT_INFO:
			replies[i].resp = NULL;
			break;
		default:
			for (int j = 0; j < i; j++) free(replies[j].resp);
			free(replies);
			return PAM_CONV_ERR;
		}
	}
	*resp = replies;
	return PAM_SUCCESS;
}

static int monitor_pam_authenticate(const char *service, const char *username,
                                    const char *password, const char *remote_host) {
	pam_handle_t *handle = NULL;
	struct pam_conv conversation = { monitor_conversation, (void *)password };
	int result = pam_start(service, username, &conversation, &handle);
	if (result != PAM_SUCCESS) return result;
	if (remote_host != NULL) pam_set_item(handle, PAM_RHOST, remote_host);
	result = pam_authenticate(handle, PAM_SILENT);
	if (result == PAM_SUCCESS) result = pam_acct_mgmt(handle, PAM_SILENT);
	pam_end(handle, result);
	return result;
}
*/
import "C"

import (
	"errors"
	"regexp"
	"runtime"
	"unsafe"
)

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)

type PAM struct{ Service string }

func (p PAM) Authenticate(username, password, remote string) error {
	if !usernamePattern.MatchString(username) || password == "" || len(password) > 1024 {
		return errors.New("authentication failed")
	}
	serviceC := C.CString(p.Service)
	userC := C.CString(username)
	passwordC := C.CString(password)
	remoteC := C.CString(remote)
	defer C.free(unsafe.Pointer(serviceC))
	defer C.free(unsafe.Pointer(userC))
	defer func() {
		C.memset(unsafe.Pointer(passwordC), 0, C.size_t(len(password)))
		C.free(unsafe.Pointer(passwordC))
	}()
	defer C.free(unsafe.Pointer(remoteC))
	result := C.monitor_pam_authenticate(serviceC, userC, passwordC, remoteC)
	runtime.KeepAlive(password)
	if result != C.PAM_SUCCESS {
		return errors.New("authentication failed")
	}
	return nil
}
