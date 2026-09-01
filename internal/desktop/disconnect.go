package desktop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danhk0612/DK-Drive/internal/config"
	"github.com/danhk0612/DK-Drive/internal/connection"
)

// Each failed profile requires its own confirmation; declining one must not
// force another or undo drives that were already safely detached.
type disconnectResult struct {
	forceMessage string
	canceled     []string
	err          error
}

func disconnectProfiles(manager *connection.Manager, profiles []config.SavedProfile, confirm func(config.SavedProfile, error) bool) disconnectResult {
	var messages []string
	var result disconnectResult
	for _, p := range profiles {
		err := manager.Disconnect(p.ID)
		if err == nil {
			continue
		}
		if !confirm(p, err) {
			result.canceled = append(result.canceled, fmt.Sprintf("%s (%s:)", p.Profile.Name, p.Profile.DriveLetter))
			continue
		}
		message, err := manager.ForceDisconnect(p.ID)
		if message != "" {
			messages = append(messages, p.Profile.Name+": "+message)
		}
		if err != nil {
			result.err = errors.Join(result.err, fmt.Errorf("%s: 강제 해제 실패: %w", p.Profile.Name, err))
		}
	}
	result.forceMessage = strings.Join(messages, "\n\n")
	return result
}

func forceDisconnectPrompt(p config.SavedProfile, err error) string {
	return fmt.Sprintf("%s (%s:) 연결을 정상 해제하지 못했습니다.\n\n%v\n\n강제로 연결을 해제할까요?\n열린 파일 작업이 중단되며, 서버에 반영되지 않은 변경 사항이 손실될 수 있습니다.\nDK-Drive에 전달된 임시 파일은 캐시에 보존하지만, 응용 프로그램/Windows 버퍼에만 남은 내용은 보존할 수 없습니다.\n자동 재업로드·복구는 하지 않습니다.\n\n예: 강제 해제 / 아니요: 연결 유지", p.Profile.Name, p.Profile.DriveLetter, err)
}
