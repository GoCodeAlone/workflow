package compliance

import (
	"fmt"
	"sync"
	"time"
)

// ControlStatus indicates the implementation state of a SOC2 control.
type SOC2ControlStatus string

const (
	ControlStatusImplemented   SOC2ControlStatus = "implemented"
	ControlStatusPartial       SOC2ControlStatus = "partial"
	ControlStatusPlanned       SOC2ControlStatus = "planned"
	ControlStatusNotApplicable SOC2ControlStatus = "not_applicable"
)

// SOC2Control represents a SOC2 Trust Services Criteria control.
type SOC2Control struct {
	ID          string            `json:"id"`       // e.g., "CC6.1", "CC7.2"
	Category    string            `json:"category"` // "Security", "Availability", "Processing Integrity", "Confidentiality", "Privacy"
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      SOC2ControlStatus `json:"status"`
	Evidence    []EvidenceItem    `json:"evidence"`
	Owner       string            `json:"owner"`
	LastReview  time.Time         `json:"last_review"`
}

// EvidenceItem represents a piece of evidence supporting a SOC2 control.
type EvidenceItem struct {
	Type        string    `json:"type"` // "automated_test", "config", "log", "document", "screenshot"
	Description string    `json:"description"`
	Source      string    `json:"source"` // file path, URL, or test name
	CollectedAt time.Time `json:"collected_at"`
	Valid       bool      `json:"valid"`
}

// ComplianceReport is the output of a SOC2 compliance assessment.
type ComplianceReport struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	TotalControls int            `json:"total_controls"`
	Implemented   int            `json:"implemented"`
	Partial       int            `json:"partial"`
	Planned       int            `json:"planned"`
	NotApplicable int            `json:"not_applicable"`
	Score         float64        `json:"score"` // percentage of implemented controls
	ByCategory    map[string]int `json:"by_category"`
	Controls      []*SOC2Control `json:"controls"`
}

// ControlRegistry manages SOC2 controls and evidence.
type ControlRegistry struct {
	mu       sync.RWMutex
	controls map[string]*SOC2Control
}

// NewControlRegistry creates a new empty control registry.
func NewControlRegistry() *ControlRegistry {
	return &ControlRegistry{
		controls: make(map[string]*SOC2Control),
	}
}

// Register adds or replaces a control in the registry.
func (r *ControlRegistry) Register(control *SOC2Control) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controls[control.ID] = control
}

// RegisterDefaults registers all standard SOC2 Trust Services Criteria controls.
func (r *ControlRegistry) RegisterDefaults() {
	defaults := defaultSOC2Controls()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range defaults {
		r.controls[c.ID] = c
	}
}

// Get retrieves a control by ID.
func (r *ControlRegistry) Get(id string) (*SOC2Control, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.controls[id]
	return c, ok
}

// List returns controls, optionally filtered by category. If category is empty,
// all controls are returned.
func (r *ControlRegistry) List(category string) []*SOC2Control {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*SOC2Control
	for _, c := range r.controls {
		if category == "" || c.Category == category {
			result = append(result, c)
		}
	}
	return result
}

// UpdateStatus changes the status of a control.
func (r *ControlRegistry) UpdateStatus(id string, status SOC2ControlStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.controls[id]
	if !ok {
		return fmt.Errorf("controls: unknown control %q", id)
	}
	c.Status = status
	c.LastReview = time.Now().UTC()
	return nil
}

// AddEvidence attaches an evidence item to a control.
func (r *ControlRegistry) AddEvidence(controlID string, evidence EvidenceItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.controls[controlID]
	if !ok {
		return fmt.Errorf("controls: unknown control %q", controlID)
	}
	c.Evidence = append(c.Evidence, evidence)
	return nil
}

// GenerateReport produces a ComplianceReport summarizing the current control states.
func (r *ControlRegistry) GenerateReport() *ComplianceReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	report := &ComplianceReport{
		GeneratedAt:   time.Now().UTC(),
		TotalControls: len(r.controls),
		ByCategory:    make(map[string]int),
	}

	for _, c := range r.controls {
		report.Controls = append(report.Controls, c)
		report.ByCategory[c.Category]++
		switch c.Status {
		case ControlStatusImplemented:
			report.Implemented++
		case ControlStatusPartial:
			report.Partial++
		case ControlStatusPlanned:
			report.Planned++
		case ControlStatusNotApplicable:
			report.NotApplicable++
		}
	}

	report.Score = r.complianceScoreLocked()
	return report
}

// ComplianceScore returns the percentage of applicable controls that are implemented.
func (r *ControlRegistry) ComplianceScore() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.complianceScoreLocked()
}

func (r *ControlRegistry) complianceScoreLocked() float64 {
	applicable := 0
	implemented := 0
	for _, c := range r.controls {
		if c.Status == ControlStatusNotApplicable {
			continue
		}
		applicable++
		if c.Status == ControlStatusImplemented {
			implemented++
		}
	}
	if applicable == 0 {
		return 0
	}
	return float64(implemented) / float64(applicable) * 100
}

// defaultSOC2Controls returns a baseline set of SOC2 Trust Services Criteria controls
// across the five trust service categories.
func defaultSOC2Controls() []*SOC2Control {
	return []*SOC2Control{
		// --- Security (Common Criteria) ---
		{
			ID:          "CC6.1",
			Category:    "Security",
			Title:       "Logical and Physical Access Controls",
			Description: "The entity implements logical access security software, infrastructure, and architectures over protected information assets to protect them from security events.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC6.2",
			Category:    "Security",
			Title:       "User Authentication",
			Description: "Prior to issuing system credentials and granting system access, the entity registers and authorizes new internal and external users.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC6.3",
			Category:    "Security",
			Title:       "Role-Based Access Control",
			Description: "The entity authorizes, modifies, or removes access to data, software, functions, and other protected information assets based on roles, responsibilities, or the system design and its changes.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC6.6",
			Category:    "Security",
			Title:       "System Boundary Protection",
			Description: "The entity implements logical access security measures to protect against threats from sources outside its system boundaries.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC6.7",
			Category:    "Security",
			Title:       "Data Encryption",
			Description: "The entity restricts the transmission, movement, and removal of information to authorized internal and external users and processes, and protects it during transmission using encryption.",
			Status:      ControlStatusPlanned,
		},
		// --- Availability ---
		{
			ID:          "A1.1",
			Category:    "Availability",
			Title:       "System Capacity Management",
			Description: "The entity maintains, monitors, and evaluates current processing capacity and use of system components to manage capacity demand and to enable the implementation of additional capacity.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "A1.2",
			Category:    "Availability",
			Title:       "Disaster Recovery Planning",
			Description: "The entity authorizes, designs, develops or acquires, implements, operates, approves, maintains, and monitors environmental protections, software, data backup processes, and recovery infrastructure.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "A1.3",
			Category:    "Availability",
			Title:       "Recovery Testing",
			Description: "The entity tests recovery plan procedures supporting system recovery to meet its objectives.",
			Status:      ControlStatusPlanned,
		},
		// --- Processing Integrity ---
		{
			ID:          "PI1.1",
			Category:    "Processing Integrity",
			Title:       "Data Validation",
			Description: "The entity implements policies and procedures over system processing to ensure that processing is complete, valid, accurate, timely, and authorized.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "PI1.2",
			Category:    "Processing Integrity",
			Title:       "Error Handling",
			Description: "The entity implements policies and procedures to detect and handle errors and exceptions in system processing, including exception handling and dead-letter queue management.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "PI1.3",
			Category:    "Processing Integrity",
			Title:       "Processing Monitoring",
			Description: "The entity monitors system processing to detect processing integrity issues and takes corrective action.",
			Status:      ControlStatusPlanned,
		},
		// --- Confidentiality ---
		{
			ID:          "C1.1",
			Category:    "Confidentiality",
			Title:       "Confidential Information Identification",
			Description: "The entity identifies and maintains confidential information to meet the entity's objectives related to confidentiality.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "C1.2",
			Category:    "Confidentiality",
			Title:       "Confidential Information Disposal",
			Description: "The entity disposes of confidential information to meet the entity's objectives related to confidentiality, including enforcement of data retention policies.",
			Status:      ControlStatusPlanned,
		},
		// --- Privacy ---
		{
			ID:          "P1.1",
			Category:    "Privacy",
			Title:       "Privacy Notice",
			Description: "The entity provides notice to data subjects about its privacy practices to meet the entity's objectives related to privacy.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "P3.1",
			Category:    "Privacy",
			Title:       "Personal Information Collection",
			Description: "Personal information is collected consistent with the entity's objectives related to privacy.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "P6.1",
			Category:    "Privacy",
			Title:       "Data Quality and Accuracy",
			Description: "The entity collects and maintains accurate, up-to-date, complete, and relevant personal information to meet the entity's objectives related to privacy.",
			Status:      ControlStatusPlanned,
		},
		// --- Additional Security Controls ---
		{
			ID:          "CC7.2",
			Category:    "Security",
			Title:       "Security Event Monitoring",
			Description: "The entity monitors system components and the operation of those components for anomalies that are indicative of malicious acts, natural disasters, and errors; and acts to investigate and remediate anomalies identified.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC7.3",
			Category:    "Security",
			Title:       "Security Incident Response",
			Description: "The entity evaluates security events to determine whether they could or have resulted in a failure of the entity to meet its objectives and, if so, takes actions to prevent or address such failures.",
			Status:      ControlStatusPlanned,
		},
		{
			ID:          "CC8.1",
			Category:    "Security",
			Title:       "Change Management",
			Description: "The entity authorizes, designs, develops, configures, documents, tests, approves, and implements changes to infrastructure, data, software, and procedures.",
			Status:      ControlStatusPlanned,
		},
	}
}
