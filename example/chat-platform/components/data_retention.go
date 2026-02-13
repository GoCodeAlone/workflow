//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var (
	retentionRecords     = make(map[string]map[string]interface{})
	retentionRecordsLock sync.Mutex
)

func Name() string {
	return "data-retention"
}

func Init(services map[string]interface{}) error {
	// Seed some sample conversations for retention checks
	retentionRecordsLock.Lock()
	defer retentionRecordsLock.Unlock()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("convo-old-%d", i)
		retentionRecords[id] = map[string]interface{}{
			"id":          id,
			"affiliateId": "aff-001",
			"programId":   "prog-001",
			"status":      "closed",
			"closedAt":    now.AddDate(0, 0, -(400 + i*30)).Format(time.RFC3339),
			"piiFields":   []interface{}{"phoneNumber", "name", "messageBody"},
		}
	}
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}

	retentionDaysF, _ := params["retentionDays"].(float64)
	retentionDays := int(retentionDaysF)
	if retentionDays <= 0 {
		retentionDays = 365
	}

	// Simulate processing delay (50-200ms)
	delay := time.Duration(50+r.Intn(150)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	switch action {
	case "check":
		affiliateId, _ := params["affiliateId"].(string)
		eligible := make([]interface{}, 0)

		retentionRecordsLock.Lock()
		for _, rec := range retentionRecords {
			if affiliateId != "" && rec["affiliateId"] != affiliateId {
				continue
			}
			if rec["status"] == "anonymized" {
				continue
			}
			closedStr, _ := rec["closedAt"].(string)
			closedAt, err := time.Parse(time.RFC3339, closedStr)
			if err != nil {
				continue
			}
			if closedAt.Before(cutoff) {
				eligible = append(eligible, map[string]interface{}{
					"id":       rec["id"],
					"closedAt": closedStr,
					"age":      int(time.Since(closedAt).Hours() / 24),
				})
			}
		}
		retentionRecordsLock.Unlock()

		return map[string]interface{}{
			"eligibleCount":  len(eligible),
			"conversations":  eligible,
			"retentionDays":  retentionDays,
			"cutoffDate":     cutoff.Format(time.RFC3339),
			"checkedAt":      time.Now().UTC().Format(time.RFC3339),
		}, nil

	case "enforce":
		processed := 0
		anonymized := 0

		retentionRecordsLock.Lock()
		for _, rec := range retentionRecords {
			if rec["status"] == "anonymized" {
				continue
			}
			closedStr, _ := rec["closedAt"].(string)
			closedAt, err := time.Parse(time.RFC3339, closedStr)
			if err != nil {
				continue
			}
			if closedAt.Before(cutoff) {
				rec["status"] = "anonymized"
				rec["phoneNumber"] = "[REDACTED]"
				rec["name"] = "[REDACTED]"
				rec["messageBody"] = "[REDACTED]"
				rec["anonymizedAt"] = time.Now().UTC().Format(time.RFC3339)
				processed++
				anonymized++
			}
		}
		retentionRecordsLock.Unlock()

		return map[string]interface{}{
			"processed":   processed,
			"anonymized":  anonymized,
			"deleted":     0,
			"enforcedAt":  time.Now().UTC().Format(time.RFC3339),
		}, nil

	case "report":
		total := 0
		active := 0
		anon := 0

		retentionRecordsLock.Lock()
		for _, rec := range retentionRecords {
			total++
			if rec["status"] == "anonymized" {
				anon++
			} else {
				active++
			}
		}
		retentionRecordsLock.Unlock()

		return map[string]interface{}{
			"totalRecords":      total,
			"activeRecords":     active,
			"anonymizedRecords": anon,
			"retentionPolicy":   fmt.Sprintf("%d days", retentionDays),
			"reportedAt":        time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'check', 'enforce', or 'report')", action)
	}
}
