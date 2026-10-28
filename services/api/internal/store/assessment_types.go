package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const AssessmentConsentVersion = "photo-ai-v1"
const AssessmentConsentContent = "AI照片辅助观察授权（试点草案）：您选择的正面、本人左侧和右侧照片仅用于本次AI辅助观察及医生复核，处理可能由配置的模型服务提供方完成。AI结果存在局限，医生复核后才向您展示，不作为诊断或治疗处方。您可撤回授权，撤回后停止后续分析和医护访问；已发送至服务提供方的数据不能立即追回。正式服务主体、提供方、保存期限和删除渠道须由院方确认。此授权独立于隐私说明、诊疗照片授权及案例展示授权。"

var (
	ErrAssessmentInput         = errors.New("invalid assessment input")
	ErrAssessmentConsent       = errors.New("assessment consent required")
	ErrAssessmentPrivacy       = errors.New("assessment privacy consent required")
	ErrAssessmentQuality       = errors.New("assessment quality rejected")
	ErrAssessmentConfiguration = errors.New("assessment pipeline unavailable")
	ErrAssessmentRateLimit     = errors.New("assessment rate limit")
)

type Assessment struct {
	ID                     string     `json:"id" gorm:"primaryKey"`
	PatientID              string     `json:"patientId"`
	ClinicID               string     `json:"clinicId"`
	AppointmentID          string     `json:"appointmentId"`
	SupersedesAssessmentID string     `json:"supersedesAssessmentId"`
	RequestKey             string     `json:"-"`
	RequestHash            string     `json:"-"`
	Version                int        `json:"version"`
	Status                 string     `json:"status"`
	ConsentGranted         bool       `json:"-"`
	ConsentEventID         string     `json:"-"`
	ReviewerEmployeeID     string     `json:"reviewerEmployeeId,omitempty"`
	SnapshotID             string     `json:"-"`
	ResultID               string     `json:"-"`
	PublishedReviewID      string     `json:"-"`
	AttemptCount           int        `json:"attemptCount"`
	MaxAttempts            int        `json:"maxAttempts"`
	FailureCode            string     `json:"failureCode"`
	Retryable              bool       `json:"retryable"`
	LeaseToken             string     `json:"-"`
	LeaseExpiresAt         *time.Time `json:"-"`
	QualityRequested       bool       `json:"-"`
	QualityRequestedAt     *time.Time `json:"-"`
	QualityLeaseToken      string     `json:"-"`
	QualityLeaseExpiresAt  *time.Time `json:"-"`
	NextAttemptAt          *time.Time `json:"nextAttemptAt"`
	SubmittedAt            *time.Time `json:"submittedAt"`
	AnalysisCompletedAt    *time.Time `json:"analysisCompletedAt"`
	ReviewedAt             *time.Time `json:"reviewedAt"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}
type AssessmentQualityItem struct {
	Code      string   `json:"code"`
	Status    string   `json:"status"`
	Score     *float64 `json:"score"`
	Threshold *float64 `json:"threshold"`
	Message   string   `json:"message"`
}
type AssessmentQuality struct {
	Status                  string                  `json:"status"`
	CheckerVersion          string                  `json:"checkerVersion"`
	ThresholdProfileVersion string                  `json:"thresholdProfileVersion"`
	CheckedAt               *time.Time              `json:"checkedAt"`
	Checks                  []AssessmentQualityItem `json:"checks"`
	Issues                  []string                `json:"issues"`
	RetakeAdvice            []string                `json:"retakeAdvice"`
}
type AssessmentPhoto struct {
	ID           string            `json:"id" gorm:"primaryKey"`
	AssessmentID string            `json:"assessmentId"`
	Angle        string            `json:"angle"`
	MediaType    string            `json:"mediaType"`
	SizeBytes    int64             `json:"sizeBytes"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	SHA256       string            `json:"sha256"`
	IsCurrent    bool              `json:"-"`
	IsFrozen     bool              `json:"-"`
	QualityJSON  string            `json:"-"`
	UploadedAt   time.Time         `json:"uploadedAt"`
	QualityCheck AssessmentQuality `json:"qualityCheck" gorm:"-"`
}
type AssessmentModel struct {
	Provider          string `json:"provider"`
	ModelID           string `json:"modelId"`
	ModelVersion      string `json:"modelVersion"`
	DeploymentVersion string `json:"deploymentVersion"`
	PromptVersion     string `json:"promptVersion"`
	AdapterVersion    string `json:"adapterVersion"`
}
type AssessmentPipelineConfig struct {
	Model                   AssessmentModel `json:"model"`
	CheckerVersion          string          `json:"checkerVersion"`
	ThresholdProfileVersion string          `json:"thresholdProfileVersion"`
}
type AssessmentEvidence struct {
	PhotoID string `json:"photoId"`
	Angle   string `json:"angle"`
}
type AssessmentObservation struct {
	ID          string               `json:"id"`
	Code        string               `json:"code"`
	Description string               `json:"description"`
	Region      string               `json:"region"`
	Certainty   string               `json:"certainty"`
	Evidence    []AssessmentEvidence `json:"evidence"`
}
type AssessmentAdvice struct {
	Recommended bool     `json:"recommended"`
	Reason      string   `json:"reason"`
	Preparation []string `json:"preparation"`
}
type AssessmentContent struct {
	Summary            string                  `json:"summary"`
	Observations       []AssessmentObservation `json:"observations"`
	Limitations        []string                `json:"limitations"`
	ConsultationAdvice AssessmentAdvice        `json:"consultationAdvice"`
}
type AssessmentAnalysis struct {
	AssessmentContent
	SchemaVersion       string          `json:"schemaVersion"`
	Model               AssessmentModel `json:"model"`
	Outcome             string          `json:"outcome"`
	RequiresHumanReview bool            `json:"requiresHumanReview"`
}
type AssessmentInputSnapshot struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	AssessmentID string    `json:"assessmentId"`
	ContentJSON  string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}
type AssessmentFrozenInput struct {
	Photos         []AssessmentPhoto        `json:"photos"`
	Configuration  AssessmentPipelineConfig `json:"configuration"`
	ConsentEventID string                   `json:"consentEventId"`
}
type AssessmentAttempt struct {
	ID                string     `json:"id" gorm:"primaryKey"`
	AssessmentID      string     `json:"assessmentId"`
	AttemptNo         int        `json:"attemptNo"`
	StartedAt         time.Time  `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt"`
	LeaseExpiresAt    time.Time  `json:"leaseExpiresAt"`
	LeaseToken        string     `json:"-"`
	ErrorCode         string     `json:"errorCode"`
	Retryable         bool       `json:"retryable"`
	ProviderRequestID string     `json:"providerRequestId"`
}
type AssessmentResult struct {
	ID              string             `json:"id" gorm:"primaryKey"`
	AssessmentID    string             `json:"assessmentId"`
	AttemptID       string             `json:"attemptId"`
	InputSnapshotID string             `json:"inputSnapshotId"`
	ContentJSON     string             `json:"-"`
	GeneratedAt     time.Time          `json:"generatedAt"`
	Analysis        AssessmentAnalysis `json:"analysis" gorm:"-"`
}
type AssessmentReview struct {
	ID                   string             `json:"id" gorm:"primaryKey"`
	AssessmentID         string             `json:"assessmentId"`
	ResultID             string             `json:"resultId"`
	Version              int                `json:"version"`
	Decision             string             `json:"decision"`
	Note                 string             `json:"note"`
	FinalJSON            *string            `json:"-"`
	ReviewedByEmployeeID string             `json:"reviewedByEmployeeId"`
	DoctorName           string             `json:"doctorName"`
	ReviewedAt           time.Time          `json:"reviewedAt"`
	SupersedesReviewID   string             `json:"supersedesReviewId"`
	FinalContent         *AssessmentContent `json:"finalContent" gorm:"-"`
}
type AssessmentEvent struct {
	ID           string    `json:"id" gorm:"primaryKey"`
	AssessmentID string    `json:"assessmentId"`
	Action       string    `json:"action"`
	ActorID      string    `json:"actorId"`
	Version      int       `json:"version"`
	Detail       string    `json:"detail"`
	CreatedAt    time.Time `json:"createdAt"`
}
type AssessmentConsentEvent struct {
	ID            string `gorm:"primaryKey"`
	AssessmentID  string
	PatientID     string
	PolicyVersion string
	PolicyContent string
	Granted       bool
	CreatedAt     time.Time
}
type AssessmentOperation struct {
	AssessmentID string `gorm:"primaryKey"`
	RequestKey   string `gorm:"primaryKey"`
	RequestHash  string
	Action       string
	ResponseJSON string
	CreatedAt    time.Time
}
type AssessmentConsent struct {
	Version        string  `json:"version"`
	Content        string  `json:"content"`
	Granted        bool    `json:"granted"`
	GrantedVersion *string `json:"grantedVersion,omitempty"`
}

// AssessmentConsentPolicy is one published version of the AI-use consent
// text (P2-8). New content is published as a new version; grants given
// under an older version stop matching and the patient must re-confirm.
// When the table is empty the seed constants above are the current policy.
type AssessmentConsentPolicy struct {
	ID        string    `gorm:"primaryKey"`
	Version   string    `gorm:"uniqueIndex"`
	Content   string
	CreatedBy string
	CreatedAt time.Time
}

// AssessmentConsentPolicyList is the admin read model: current policy plus
// the version history, newest first.
type AssessmentConsentPolicyList struct {
	Policy  AssessmentConsentPolicy
	History []AssessmentConsentPolicy
}
type AssessmentPublicReview struct {
	ID           string             `json:"id"`
	Version      int                `json:"version"`
	Decision     string             `json:"decision"`
	Note         string             `json:"note"`
	DoctorName   string             `json:"doctorName"`
	ReviewedAt   time.Time          `json:"reviewedAt"`
	FinalContent *AssessmentContent `json:"finalContent"`
}
type AssessmentView struct {
	Task            Assessment              `json:"task"`
	Photos          []AssessmentPhoto       `json:"photos"`
	Consent         AssessmentConsent       `json:"consent"`
	PublishedReview *AssessmentPublicReview `json:"publishedReview"`
	Result          *AssessmentResult       `json:"result,omitempty"`
	Reviews         []AssessmentReview      `json:"reviews,omitempty"`
	Attempts        []AssessmentAttempt     `json:"attempts,omitempty"`
}
type AssessmentCreate struct {
	ClinicID               string `json:"clinicId"`
	AppointmentID          string `json:"appointmentId,omitempty"`
	SupersedesAssessmentID string `json:"supersedesAssessmentId,omitempty"`
	RequestKey             string `json:"requestKey"`
}
type AssessmentChange struct {
	Version            int                `json:"version"`
	PolicyVersion      string             `json:"policyVersion,omitempty"`
	Granted            bool               `json:"granted,omitempty"`
	ReviewerEmployeeID string             `json:"reviewerEmployeeId,omitempty"`
	ResultID           string             `json:"resultId,omitempty"`
	Decision           string             `json:"decision,omitempty"`
	Note               string             `json:"note,omitempty"`
	FinalContent       *AssessmentContent `json:"finalContent,omitempty"`
}
type AssessmentFilter struct {
	Status   string
	ClinicID string
	Query    string
	Page     int
	PageSize int
}
type AssessmentPage struct {
	Data []Assessment `json:"data"`
	Meta struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"meta"`
}
type AssessmentProvider interface {
	Check(context.Context, AssessmentFrozenInput, map[string][]byte) (map[string]AssessmentQuality, error)
	Analyze(context.Context, AssessmentFrozenInput, map[string][]byte, string) (AssessmentAnalysis, string, error)
}
type AssessmentProviderError struct {
	Code       string
	Retryable  bool
	RetryAfter time.Duration
}

func (e *AssessmentProviderError) Error() string { return e.Code }

type AssessmentService struct {
	repo          *MySQL
	media         Media
	provider      AssessmentProvider
	configuration AssessmentPipelineConfig
}

func NewAssessmentService(repo *MySQL, media Media, provider AssessmentProvider, cfg AssessmentPipelineConfig) *AssessmentService {
	return &AssessmentService{repo: repo, media: media, provider: provider, configuration: cfg}
}
func assessmentHash(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
