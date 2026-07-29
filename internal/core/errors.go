package core

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *AppError) Error() string {
	return e.Message
}

var (
	ErrServiceNotFound = &AppError{Code: "SERVICE_NOT_FOUND", Message: "服务未找到"}
	ErrServiceRunning  = &AppError{Code: "SERVICE_RUNNING", Message: "服务正在运行"}
	ErrServiceStopped  = &AppError{Code: "SERVICE_STOPPED", Message: "服务已停止"}
	ErrPortInUse       = &AppError{Code: "PORT_IN_USE", Message: "端口被占用"}
	ErrNotInstalled    = &AppError{Code: "NOT_INSTALLED", Message: "服务未安装"}
	ErrDownloadFailed  = &AppError{Code: "DOWNLOAD_FAILED", Message: "下载失败"}
	ErrExtractFailed   = &AppError{Code: "EXTRACT_FAILED", Message: "解压失败"}
	ErrRuntimeNotFound = &AppError{Code: "RUNTIME_NOT_FOUND", Message: "运行时未找到"}
	ErrProjectNotFound = &AppError{Code: "PROJECT_NOT_FOUND", Message: "项目未找到"}
	ErrNotFound        = &AppError{Code: "NOT_FOUND", Message: "资源未找到"}
	ErrEnvProfileInUse = &AppError{Code: "ENV_PROFILE_IN_USE", Message: "该环境被项目引用，禁止删除"}
)
