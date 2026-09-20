package core

import "time"

const SecretMask = "********"

type FieldType string

const (
	FieldString     FieldType = "string"
	FieldText       FieldType = "text"
	FieldSecret     FieldType = "secret"
	FieldInt        FieldType = "int"
	FieldBool       FieldType = "bool"
	FieldSelect     FieldType = "select"
	FieldStringList FieldType = "list"
	FieldPath       FieldType = "path"
	FieldPort       FieldType = "port"
)

type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Field struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Type        FieldType      `json:"type"`
	Required    bool           `json:"required"`
	Secret      bool           `json:"secret"`
	Default     any            `json:"default,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Help        string         `json:"help,omitempty"`
	Options     []FieldOption  `json:"options,omitempty"`
	Group       string         `json:"group,omitempty"`
	Advanced    bool           `json:"advanced,omitempty"`
	ShowIf      map[string]any `json:"showIf,omitempty"`
	LocalOnly   bool           `json:"localOnly,omitempty"`
}

const (
	CapRestore = "restore"
	CapBrowse  = "browse"
	CapIngest  = "ingest"
	CapTest    = "test"
)

type DriverSpec struct {
	Kind         string   `json:"kind"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	Category     string   `json:"category"`
	Fields       []Field  `json:"fields"`
	Capabilities []string `json:"capabilities"`
	Tools        []string `json:"tools,omitempty"`
	DocsURL      string   `json:"docsUrl,omitempty"`
}

func (s DriverSpec) Has(capability string) bool {
	for _, c := range s.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func (s DriverSpec) SecretFields() []string {
	var out []string
	for _, f := range s.Fields {
		if f.Secret || f.Type == FieldSecret {
			out = append(out, f.Name)
		}
	}
	return out
}

type Compression string

const (
	CompressionNone Compression = "none"
	CompressionGzip Compression = "gzip"
	CompressionZstd Compression = "zstd"
)

type Encryption string

const (
	EncryptionNone Encryption = "none"
	EncryptionAge  Encryption = "age"
)

type Retention struct {
	KeepLast    int `json:"keepLast"`
	KeepHourly  int `json:"keepHourly"`
	KeepDaily   int `json:"keepDaily"`
	KeepWeekly  int `json:"keepWeekly"`
	KeepMonthly int `json:"keepMonthly"`
	KeepYearly  int `json:"keepYearly"`
	MaxAgeDays  int `json:"maxAgeDays"`
}

func (r Retention) IsZero() bool {
	return r == Retention{}
}

type Source struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Description   string     `json:"description"`
	Config        Config     `json:"config"`
	HostID        string     `json:"hostId"`
	HostName      string     `json:"hostName,omitempty"`
	Tags          []string   `json:"tags"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastTestAt    *time.Time `json:"lastTestAt,omitempty"`
	LastTestOK    *bool      `json:"lastTestOk,omitempty"`
	LastTestError string     `json:"lastTestError,omitempty"`
	JobCount      int        `json:"jobCount"`
}

type Destination struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Description   string     `json:"description"`
	Config        Config     `json:"config"`
	Tags          []string   `json:"tags"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastTestAt    *time.Time `json:"lastTestAt,omitempty"`
	LastTestOK    *bool      `json:"lastTestOk,omitempty"`
	LastTestError string     `json:"lastTestError,omitempty"`
	JobCount      int        `json:"jobCount"`
	UsedBytes     int64      `json:"usedBytes"`
	ArtifactCount int        `json:"artifactCount"`
}

const (
	EventRunSuccess      = "run.success"
	EventRunFailed       = "run.failed"
	EventRunWarning      = "run.warning"
	EventJobOverdue      = "job.overdue"
	EventArtifactMissing = "artifact.missing"
	EventPruneDone       = "prune.done"
	EventRestoreDone     = "restore.done"
	EventRestoreFailed   = "restore.failed"
)

var AllEvents = []string{
	EventRunSuccess, EventRunFailed, EventRunWarning, EventJobOverdue,
	EventArtifactMissing, EventPruneDone, EventRestoreDone, EventRestoreFailed,
}

type Job struct {
	ID                      string      `json:"id"`
	Slug                    string      `json:"slug"`
	Name                    string      `json:"name"`
	Description             string      `json:"description"`
	SourceID                string      `json:"sourceId"`
	DestinationIDs          []string    `json:"destinationIds"`
	Schedule                string      `json:"schedule"`
	Timezone                string      `json:"timezone"`
	Enabled                 bool        `json:"enabled"`
	Compression             Compression `json:"compression"`
	CompressionLevel        int         `json:"compressionLevel"`
	Encryption              Encryption  `json:"encryption"`
	EncryptionPassphrase    string      `json:"encryptionPassphrase"`
	Retention               Retention   `json:"retention"`
	NotificationChannelIDs  []string    `json:"notificationChannelIds"`
	NotifyOn                []string    `json:"notifyOn"`
	TimeoutMinutes          int         `json:"timeoutMinutes"`
	Retries                 int         `json:"retries"`
	RetryDelaySeconds       int         `json:"retryDelaySeconds"`
	PreCommand              string      `json:"preCommand"`
	PostCommand             string      `json:"postCommand"`
	VerifyAfterUpload       bool        `json:"verifyAfterUpload"`
	ExpectedIntervalMinutes int         `json:"expectedIntervalMinutes"`
	Tags                    []string    `json:"tags"`
	CreatedAt               time.Time   `json:"createdAt"`
	UpdatedAt               time.Time   `json:"updatedAt"`
	SourceName              string      `json:"sourceName,omitempty"`
	SourceKind              string      `json:"sourceKind,omitempty"`
	DestinationNames        []string    `json:"destinationNames,omitempty"`
	LastRun                 *RunSummary `json:"lastRun,omitempty"`
	NextRunAt               *time.Time  `json:"nextRunAt,omitempty"`
	Overdue                 bool        `json:"overdue"`
	ArtifactCount           int         `json:"artifactCount"`
	TotalBytes              int64       `json:"totalBytes"`
}

type RunKind string

const (
	RunBackup  RunKind = "backup"
	RunRestore RunKind = "restore"
	RunPrune   RunKind = "prune"
	RunVerify  RunKind = "verify"
	RunIngest  RunKind = "ingest"
)

type RunTrigger string

const (
	TriggerSchedule RunTrigger = "schedule"
	TriggerManual   RunTrigger = "manual"
	TriggerAPI      RunTrigger = "api"
	TriggerIngest   RunTrigger = "ingest"
	TriggerRetry    RunTrigger = "retry"
)

type RunStatus string

const (
	RunQueued   RunStatus = "queued"
	RunRunning  RunStatus = "running"
	RunSuccess  RunStatus = "success"
	RunWarning  RunStatus = "warning"
	RunFailed   RunStatus = "failed"
	RunCanceled RunStatus = "canceled"
)

func (s RunStatus) Terminal() bool {
	switch s {
	case RunSuccess, RunWarning, RunFailed, RunCanceled:
		return true
	}
	return false
}

type StageStatus string

const (
	StagePending StageStatus = "pending"
	StageRunning StageStatus = "running"
	StageSuccess StageStatus = "success"
	StageFailed  StageStatus = "failed"
	StageSkipped StageStatus = "skipped"
)

type Stage struct {
	Name       string      `json:"name"`
	Status     StageStatus `json:"status"`
	StartedAt  *time.Time  `json:"startedAt,omitempty"`
	FinishedAt *time.Time  `json:"finishedAt,omitempty"`
	Message    string      `json:"message,omitempty"`
}

type Run struct {
	ID          string            `json:"id"`
	JobID       string            `json:"jobId"`
	JobSlug     string            `json:"jobSlug"`
	JobName     string            `json:"jobName"`
	Kind        RunKind           `json:"kind"`
	Trigger     RunTrigger        `json:"trigger"`
	Status      RunStatus         `json:"status"`
	QueuedAt    time.Time         `json:"queuedAt"`
	StartedAt   *time.Time        `json:"startedAt,omitempty"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
	DurationMS  int64             `json:"durationMs"`
	Bytes       int64             `json:"bytes"`
	RawBytes    int64             `json:"rawBytes"`
	SHA256      string            `json:"sha256,omitempty"`
	Filename    string            `json:"filename,omitempty"`
	Error       string            `json:"error,omitempty"`
	Stages      []Stage           `json:"stages"`
	ArtifactIDs []string          `json:"artifactIds"`
	Attempt     int               `json:"attempt"`
	Meta        map[string]string `json:"meta,omitempty"`
	CreatedBy   string            `json:"createdBy,omitempty"`
}

type RunSummary struct {
	ID         string     `json:"id"`
	Kind       RunKind    `json:"kind"`
	Status     RunStatus  `json:"status"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	DurationMS int64      `json:"durationMs"`
	Bytes      int64      `json:"bytes"`
	Error      string     `json:"error,omitempty"`
}

func (r Run) Summary() RunSummary {
	return RunSummary{
		ID: r.ID, Kind: r.Kind, Status: r.Status, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, DurationMS: r.DurationMS, Bytes: r.Bytes, Error: r.Error,
	}
}

type ArtifactStatus string

const (
	ArtifactPresent ArtifactStatus = "present"
	ArtifactMissing ArtifactStatus = "missing"
	ArtifactPruned  ArtifactStatus = "pruned"
	ArtifactDeleted ArtifactStatus = "deleted"
)

type Artifact struct {
	ID              string            `json:"id"`
	JobID           string            `json:"jobId"`
	JobSlug         string            `json:"jobSlug"`
	JobName         string            `json:"jobName,omitempty"`
	RunID           string            `json:"runId"`
	DestinationID   string            `json:"destinationId"`
	DestinationName string            `json:"destinationName,omitempty"`
	DestinationKind string            `json:"destinationKind,omitempty"`
	Path            string            `json:"path"`
	Filename        string            `json:"filename"`
	Size            int64             `json:"size"`
	SHA256          string            `json:"sha256"`
	Compression     Compression       `json:"compression"`
	Encryption      Encryption        `json:"encryption"`
	SourceKind      string            `json:"sourceKind"`
	Extension       string            `json:"extension"`
	Status          ArtifactStatus    `json:"status"`
	CreatedAt       time.Time         `json:"createdAt"`
	VerifiedAt      *time.Time        `json:"verifiedAt,omitempty"`
	DeletedAt       *time.Time        `json:"deletedAt,omitempty"`
	Meta            map[string]string `json:"meta,omitempty"`
}

type NotificationChannel struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Config     Config     `json:"config"`
	Enabled    bool       `json:"enabled"`
	Events     []string   `json:"events"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
}

type UserRole string

const (
	RoleAdmin  UserRole = "admin"
	RoleViewer UserRole = "viewer"
)

type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        UserRole   `json:"role"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

const (
	ScopeAdmin  = "admin"
	ScopeRead   = "read"
	ScopeIngest = "ingest"
	ScopeRun    = "run"
)

type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	JobSlugs   []string   `json:"jobSlugs"`
	CreatedAt  time.Time  `json:"createdAt"`
	CreatedBy  string     `json:"createdBy,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type AuditEntry struct {
	ID         string         `json:"id"`
	Time       time.Time      `json:"time"`
	ActorID    string         `json:"actorId"`
	ActorLabel string         `json:"actorLabel"`
	Action     string         `json:"action"`
	ObjectType string         `json:"objectType"`
	ObjectID   string         `json:"objectId"`
	ObjectName string         `json:"objectName"`
	Details    map[string]any `json:"details,omitempty"`
	IP         string         `json:"ip,omitempty"`
}

type Settings struct {
	SiteName            string    `json:"siteName"`
	BaseURL             string    `json:"baseUrl"`
	DefaultTimezone     string    `json:"defaultTimezone"`
	MaxConcurrentRuns   int       `json:"maxConcurrentRuns"`
	DefaultRetention    Retention `json:"defaultRetention"`
	RunHistoryDays      int       `json:"runHistoryDays"`
	AuditHistoryDays    int       `json:"auditHistoryDays"`
	OverdueCheckMinutes int       `json:"overdueCheckMinutes"`
	DefaultNotifyOn     []string  `json:"defaultNotifyOn"`
}

type ToolStatus struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Path      string   `json:"path,omitempty"`
	Version   string   `json:"version,omitempty"`
	UsedBy    []string `json:"usedBy"`
}

type DestinationUsage struct {
	DestinationID   string `json:"destinationId"`
	DestinationName string `json:"destinationName"`
	Kind            string `json:"kind"`
	Bytes           int64  `json:"bytes"`
	Artifacts       int    `json:"artifacts"`
}

type DailyStat struct {
	Date    string `json:"date"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Bytes   int64  `json:"bytes"`
}

type DashboardStats struct {
	Jobs           int                `json:"jobs"`
	JobsEnabled    int                `json:"jobsEnabled"`
	JobsOverdue    int                `json:"jobsOverdue"`
	JobsFailing    int                `json:"jobsFailing"`
	RunsRunning    int                `json:"runsRunning"`
	Runs24hSuccess int                `json:"runs24hSuccess"`
	Runs24hFailed  int                `json:"runs24hFailed"`
	Artifacts      int                `json:"artifacts"`
	TotalBytes     int64              `json:"totalBytes"`
	Destinations   []DestinationUsage `json:"destinations"`
	Daily          []DailyStat        `json:"daily"`
	RecentRuns     []Run              `json:"recentRuns"`
	Upcoming       []Job              `json:"upcoming"`
	ProblemJobs    []Job              `json:"problemJobs"`
}

type VersionInfo struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate string    `json:"buildDate"`
	GoVersion string    `json:"goVersion"`
	StartedAt time.Time `json:"startedAt"`
}

const CapRemote = "remote"

const HostConfigKey = "_host"

type HostAuth string

const (
	HostAuthKey      HostAuth = "key"
	HostAuthPassword HostAuth = "password"
)

type Host struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Address        string     `json:"address"`
	Port           int        `json:"port"`
	User           string     `json:"user"`
	Auth           HostAuth   `json:"auth"`
	PrivateKey     string     `json:"privateKey"`
	KeyPassphrase  string     `json:"keyPassphrase"`
	Password       string     `json:"password"`
	PublicKey      string     `json:"publicKey"`
	HostKey        string     `json:"hostKey"`
	Sudo           bool       `json:"sudo"`
	ConnectTimeout int        `json:"connectTimeoutSeconds"`
	Tags           []string   `json:"tags"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	LastTestAt     *time.Time `json:"lastTestAt,omitempty"`
	LastTestOK     *bool      `json:"lastTestOk,omitempty"`
	LastTestError  string     `json:"lastTestError,omitempty"`
	LastSeenOS     string     `json:"lastSeenOs,omitempty"`
	Tools          []string   `json:"tools,omitempty"`
	SourceCount    int        `json:"sourceCount"`
}

var HostSecretFields = []string{"privateKey", "keyPassphrase", "password"}

func (h Host) Masked() Host {
	out := h
	if out.PrivateKey != "" {
		out.PrivateKey = SecretMask
	}
	if out.KeyPassphrase != "" {
		out.KeyPassphrase = SecretMask
	}
	if out.Password != "" {
		out.Password = SecretMask
	}
	return out
}

func (c Config) WithHost(h *Host) Config {
	out := c.Clone()
	if h == nil {
		delete(out, HostConfigKey)
		return out
	}
	out[HostConfigKey] = *h
	return out
}

func (c Config) Host() *Host {
	v, ok := c[HostConfigKey]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case Host:
		return &t
	case *Host:
		return t
	}
	return nil
}
