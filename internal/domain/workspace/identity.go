package workspace

type Identity struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Username       string `json:"username"`
	Authentication string `json:"authentication"`
	KeyID          string `json:"keyId"`
	UseAgent       bool   `json:"useAgent,omitempty"`
	Version        int64  `json:"version"`
}
type SealedIdentity struct {
	Identity
	Cipher []byte `json:"-"`
}

func ValidIdentity(i Identity) bool {
	return idPattern.MatchString(i.ID) && cleanText(i.Name, 64) && cleanText(i.Username, 64) && (i.Authentication == "password" || i.Authentication == "key") && (i.KeyID == "" || idPattern.MatchString(i.KeyID)) && (i.Authentication == "key" || i.KeyID == "") && (!i.UseAgent || (i.Authentication == "key" && i.KeyID == ""))
}
