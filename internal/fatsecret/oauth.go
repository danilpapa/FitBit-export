package fatsecret

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// sign adds the OAuth 1.0 parameters and an HMAC-SHA1 signature to params.
func (c *Client) sign(method string, endpoint string, params url.Values, tokenSecret string) {
	params.Set("oauth_consumer_key", c.config.ConsumerKey)
	params.Set("oauth_nonce", c.nonce())
	params.Set("oauth_signature_method", "HMAC-SHA1")
	params.Set("oauth_timestamp", strconv.FormatInt(c.now().Unix(), 10))
	params.Set("oauth_version", "1.0")
	params.Set("oauth_signature", signature(method, endpoint, params, c.config.ConsumerSecret, tokenSecret))
}

func signature(method string, endpoint string, params url.Values, consumerSecret string, tokenSecret string) string {
	base := method + "&" + encode(endpoint) + "&" + encode(encodeParams(params))
	mac := hmac.New(sha1.New, []byte(encode(consumerSecret)+"&"+encode(tokenSecret)))
	mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// encodeParams sorts parameters by name, then value, as OAuth 1.0 requires.
func encodeParams(params url.Values) string {
	type pair struct{ name, value string }

	var pairs []pair
	for name, values := range params {
		for _, value := range values {
			pairs = append(pairs, pair{encode(name), encode(value)})
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		if a.name != b.name {
			return strings.Compare(a.name, b.name)
		}
		return strings.Compare(a.value, b.value)
	})

	encoded := make([]string, len(pairs))
	for i, p := range pairs {
		encoded[i] = p.name + "=" + p.value
	}
	return strings.Join(encoded, "&")
}

// encode applies RFC 3986 percent-encoding.
func encode(value string) string {
	var builder strings.Builder
	for _, c := range []byte(value) {
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			builder.WriteByte(c)
		} else {
			fmt.Fprintf(&builder, "%%%02X", c)
		}
	}
	return builder.String()
}

func randomNonce() string {
	buffer := make([]byte, 16)
	rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
