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
func disconnectProfiles(manager *connection.Manager, profiles []config.SavedProfile, confirm func(config.SavedProfile, error) bool) (string, error) {
	var messages []string
	var result error
	for _, p := range profiles {
		err := manager.Disconnect(p.ID)
		if err == nil {
			continue
		}
		if !confirm(p, err) {
			result = errors.Join(result, fmt.Errorf("%s: 강제 해제 취소, 연결 유지: %w", p.Profile.Name, err))
			continue
		}
		message, err := manager.ForceDisconnect(p.ID)
		if message != "" {
			messages = append(messages, p.Profile.Name+": "+message)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("%s: 강제 해제 실패: %w", p.Profile.Name, err))
		}
	}
	return strings.Join(messages, "\n\n"), result
}

func forceDisconnectPrompt(p config.SavedProfile, err error) string {
	return fmt.Sprintf("%s (%s:) 연결을 정상 해제하지 못했습니다.\n\n%v\n\n강제로 연결을 해제할까요?\n열린 파일 작업이 중단되며, 서버에 반영되지 않은 변경 사항이 손실될 수 있습니다.\nDKDrive에 전달된 임시 파일은 캐시에 보존하지만, 응용 프로그램/Windows 버퍼에만 남은 내용은 보존할 수 없습니다.\n자동 재업로드·복구는 하지 않습니다.\n\n예: 강제 해제 / 아니요: 연결 유지", p.Profile.Name, p.Profile.DriveLetter, err)
}
