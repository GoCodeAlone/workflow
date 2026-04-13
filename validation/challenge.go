package validation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"time"
)

// wordCount is the number of words in bip39Subset.
const wordCount = 256

// bip39Subset is a fixed 256-word subset of the BIP-39 English word list used
// for generating readable challenge tokens. Words are chosen to be short,
// unambiguous, and easy to type.
var bip39Subset = [wordCount]string{
	"able", "about", "above", "absent", "absorb", "abstract", "absurd", "abuse",
	"access", "account", "accuse", "achieve", "acid", "acoustic", "acquire", "across",
	"act", "action", "actor", "actual", "adapt", "add", "addict", "address",
	"adjust", "admit", "adult", "advance", "advice", "aerobic", "afford", "afraid",
	"again", "agent", "agree", "ahead", "aim", "airport", "aisle", "alarm",
	"album", "alcohol", "alert", "alien", "all", "alley", "allow", "almost",
	"alone", "alpha", "already", "alter", "always", "amateur", "amazing", "among",
	"amount", "amused", "analyst", "anchor", "ancient", "anger", "angle", "angry",
	"animal", "ankle", "announce", "annual", "another", "answer", "antenna", "antique",
	"anxiety", "apart", "apple", "approve", "april", "arch", "arctic", "argue",
	"arm", "armed", "armor", "army", "around", "arrange", "arrest", "arrive",
	"arrow", "art", "article", "artist", "aspect", "assault", "asset", "assist",
	"assume", "athlete", "atom", "attack", "attend", "attitude", "attract", "auction",
	"august", "aunt", "author", "auto", "autumn", "average", "avocado", "avoid",
	"awake", "aware", "away", "awesome", "awful", "awkward", "axis", "baby",
	"balance", "bamboo", "banana", "banner", "barely", "bargain", "barrel", "base",
	"basic", "basket", "battle", "beach", "beauty", "because", "become", "beef",
	"before", "begin", "behave", "behind", "believe", "below", "belt", "bench",
	"benefit", "best", "betray", "better", "between", "beyond", "bicycle", "bind",
	"biology", "bird", "birth", "bitter", "black", "blade", "blame", "blanket",
	"blast", "bleak", "bless", "blind", "blood", "blossom", "blouse", "blue",
	"blur", "blush", "board", "boat", "body", "boil", "bomb", "bone",
	"bonus", "book", "boost", "border", "boring", "borrow", "boss", "bottom",
	"bounce", "boy", "brain", "brand", "brave", "breeze", "brick", "bridge",
	"brief", "bright", "bring", "brisk", "broccoli", "broken", "bronze", "broom",
	"brother", "brown", "brush", "bubble", "buddy", "budget", "buffalo", "build",
	"bulk", "bullet", "bundle", "bunker", "burden", "burger", "burst", "bus",
	"business", "busy", "butter", "buyer", "buzz", "cabbage", "cabin", "cable",
	"cactus", "cage", "cake", "call", "calm", "camera", "camp", "canal",
	"cancel", "candy", "cannon", "canvas", "canyon", "capable", "capital", "captain",
	"carbon", "card", "cargo", "carpet", "carry", "cart", "case", "castle",
	"catalog", "catch", "category", "cause", "caution", "cave", "ceiling", "celery",
}

// timeBucketBytes returns the 1-hour bucket for t as a big-endian uint64 byte slice.
func timeBucketBytes(t time.Time) []byte {
	bucket := uint64(t.UTC().Unix() / 3600) //nolint:gosec // G115: Unix() is always non-negative
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, bucket)
	return b
}

// computeToken computes the 3-word token for the given secret, hash, and time.
func computeToken(secret, hash string, t time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(hash))
	mac.Write(timeBucketBytes(t))
	sum := mac.Sum(nil)
	w0 := binary.BigEndian.Uint16(sum[0:2]) % wordCount
	w1 := binary.BigEndian.Uint16(sum[2:4]) % wordCount
	w2 := binary.BigEndian.Uint16(sum[4:6]) % wordCount
	return bip39Subset[w0] + "-" + bip39Subset[w1] + "-" + bip39Subset[w2]
}

// GenerateChallenge generates a 3-word HMAC challenge token for the given
// rejection hash using the admin secret, anchored to the 1-hour bucket of t.
// Pass time.Now() for normal use; pass a fixed time in tests for determinism.
//
// adminSecret should come from an environment variable (e.g. WFCTL_ADMIN_SECRET).
func GenerateChallenge(adminSecret, rejectionHash string, t time.Time) string {
	return computeToken(adminSecret, rejectionHash, t)
}

// VerifyChallenge returns true if token matches the expected challenge for the
// given rejection hash at time t. It checks both the current and previous
// 1-hour buckets to provide a grace period across bucket boundaries.
// Comparison is constant-time to prevent timing side-channel attacks.
func VerifyChallenge(adminSecret, rejectionHash, token string, t time.Time) bool {
	if hmac.Equal([]byte(token), []byte(computeToken(adminSecret, rejectionHash, t))) {
		return true
	}
	return hmac.Equal([]byte(token), []byte(computeToken(adminSecret, rejectionHash, t.Add(-time.Hour))))
}

// TokenFromParts joins three BIP-39 words with hyphens (inverse of parsing).
func TokenFromParts(words []string) string {
	return strings.Join(words, "-")
}
