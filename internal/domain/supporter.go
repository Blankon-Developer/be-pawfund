package domain

type Supporter struct {
	User
	Name           string
	ImageObjectKey *string
}

// SupporterProfileReplacement contains every required supporter profile field
// for a full PUT replacement, plus an optional image change.
type SupporterProfileReplacement struct {
	Name           string
	Email          string
	ImageObjectKey ImageObjectKeyUpdate
}
