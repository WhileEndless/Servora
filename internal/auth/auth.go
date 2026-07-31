package auth

type Authenticator interface {
	Authenticate(username, password, remote string) error
}
