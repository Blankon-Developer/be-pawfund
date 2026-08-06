package domain

type AuthProfile struct {
	Name           string
	Role           UserRole
	ImageObjectKey *string
}
