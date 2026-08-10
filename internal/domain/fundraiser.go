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

// ImageObjectKeyUpdate represents an optional image change. An omitted field
// preserves the existing image; null clears it.
type ImageObjectKeyUpdate struct {
	Set   bool
	Value *string
}

// FundraiserProfileReplacement contains every required fundraiser profile
// field for a full PUT replacement, plus an optional image change.
type FundraiserProfileReplacement struct {
	Name           string
	Email          string
	ImageObjectKey ImageObjectKeyUpdate
	ContactName    string
	ContactPhone   string
	SocialURL      string
	Country        string
	ZipCode        string
}
