package validation

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// bip39Subset is a fixed 256-word subset of the BIP-39 English word list used
// for generating readable challenge tokens. Words are chosen to be short,
// unambiguous, and easy to type.
var bip39Subset = [256]string{
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
}

// timeBucket returns the current 1-hour time bucket as a string.
func timeBucket(t time.Time) string {
	return fmt.Sprintf("%d", t.UTC().Unix()/3600)
}

// computeToken computes the 3-word token for the given hash and time bucket.
func computeToken(secret, hash, bucket string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(hash))
	mac.Write([]byte(bucket))
	sum := mac.Sum(nil)
	return fmt.Sprintf("%s-%s-%s", bip39Subset[sum[0]], bip39Subset[sum[1]], bip39Subset[sum[2]])
}

// GenerateChallenge generates a 3-word HMAC challenge token for the given
// rejection hash using the admin secret. The token is valid for the current
// 1-hour window and the next (giving at least 1h of usability).
//
// adminSecret should come from an environment variable (e.g. WFCTL_ADMIN_SECRET).
func GenerateChallenge(adminSecret, rejectionHash string) string {
	return computeToken(adminSecret, rejectionHash, timeBucket(time.Now()))
}

// VerifyChallenge returns true if token matches the expected challenge for the
// given rejection hash. It checks both the current and previous 1-hour buckets
// to provide a grace period across bucket boundaries.
func VerifyChallenge(adminSecret, rejectionHash, token string) bool {
	now := time.Now()
	if token == computeToken(adminSecret, rejectionHash, timeBucket(now)) {
		return true
	}
	prev := now.Add(-time.Hour)
	return token == computeToken(adminSecret, rejectionHash, timeBucket(prev))
}

// TokenFromParts joins three BIP-39 words with hyphens (inverse of parsing).
func TokenFromParts(words []string) string {
	return strings.Join(words, "-")
}
