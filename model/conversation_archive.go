package model

// ConversationArchive is the searchable manifest for an immutable conversation
// record stored outside the transactional database. It deliberately contains no
// request or response bodies, so conversational traffic cannot grow the primary
// database through TOAST/TEXT payloads again.
type ConversationArchive struct {
	Id                   int64  `json:"id" gorm:"primarykey;autoIncrement"`
	ArchiveId            string `json:"archive_id" gorm:"type:varchar(64);uniqueIndex"`
	RequestId            string `json:"request_id" gorm:"type:varchar(64);index"`
	UserId               int    `json:"user_id" gorm:"index"`
	Username             string `json:"username" gorm:"index"`
	TokenId              int    `json:"token_id" gorm:"index"`
	TokenName            string `json:"token_name"`
	ChannelId            int    `json:"channel_id" gorm:"index"`
	OriginModelName      string `json:"origin_model_name" gorm:"index"`
	UpstreamModelName    string `json:"upstream_model_name"`
	ClientProtocol       string `json:"client_protocol" gorm:"type:varchar(64);index"`
	UpstreamProtocol     string `json:"upstream_protocol" gorm:"type:varchar(64)"`
	RequestPath          string `json:"request_path" gorm:"type:text"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;index"`
	CompletedAt          int64  `json:"completed_at" gorm:"bigint"`
	StatusCode           int    `json:"status_code"`
	IsStream             bool   `json:"is_stream"`
	Complete             bool   `json:"complete" gorm:"index"`
	TrainingConsent      string `json:"training_consent" gorm:"type:varchar(32);index"`
	RecordPath           string `json:"record_path" gorm:"type:text"`
	RecordSha256         string `json:"record_sha256" gorm:"type:char(64)"`
	RequestBytes         int64  `json:"request_bytes"`
	ResponseBytes        int64  `json:"response_bytes"`
	StoredBytes          int64  `json:"stored_bytes"`
	RequestConversion    string `json:"request_conversion" gorm:"type:text"`
	ErrorCode            string `json:"error_code" gorm:"type:varchar(128)"`
	ArchiveFormatVersion int    `json:"archive_format_version"`
}

func InsertConversationArchive(archive *ConversationArchive) error {
	if archive == nil {
		return nil
	}
	return DB.Create(archive).Error
}
