package domain

type Fundraiser struct {
	User
	Name           string
	ImageObjectKey *string
	ContactName    string
	ContactPhone   string
	SocialURL      *string
	Country        string
	ZipCode        string
}
