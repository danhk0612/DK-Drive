# DKDrive

**DKDrive (Direct Konnect Drive)**는 SFTP, WebDAV, FTP/FTPS 원격 저장소를
Windows 드라이브 문자로 마운트하는 경량 네트워크 드라이브 클라이언트입니다.

## 현재 상태

현재 버전은 `0.2.0-dev`입니다. WinFsp 메모리 드라이브와 실제 SFTP 드라이브의
연결·읽기·쓰기·이름 변경·이동·삭제·읽기 전용·자동 재연결을 Windows
탐색기와 메모장에서 검증했습니다. 현재 SFTP 기본 기능을 완성하는 단계입니다.

## 목표 기능

- SFTP 비밀번호/개인키 인증
- WebDAV HTTP/HTTPS
- FTP, Explicit FTPS 및 가능한 경우 Implicit FTPS
- 여러 연결 프로필과 드라이브 문자
- 연결별 읽기 전용, 자동 연결, 자동 재연결
- 로컬 읽기 캐시와 안전한 쓰기 반영
- 트레이 상주와 Windows 자동 시작
- 서버측 이름 변경, 이동, 삭제 최적화

## 요구 환경

- Windows 10/11 64-bit
- Go 1.26 이상(소스 빌드 시)
- WinFsp 2.1 이상

WinFsp가 설치되지 않은 경우 다음 명령으로 설치할 수 있습니다.

```powershell
winget install --exact --id WinFsp.WinFsp
```

## 빌드

```powershell
go test ./...
go vet ./...
go build -o bin/dkdrive.exe ./cmd/dkdrive
```

현재 실행 파일은 버전과 개발 상태만 표시합니다.

```powershell
.\bin\dkdrive.exe --version
```

WinFsp 메모리 마운트 검증 방법은
[WinFsp 메모리 마운트 기술 검증](docs/winfsp-memory-spike.md)을 참고하세요.

SFTP 연결과 원격 파일 읽기 검증 방법은
[SFTP 연결 기술 검증](docs/sftp-connection-spike.md)을 참고하세요.

SFTP 원격 경로를 WinFsp 드라이브로 마운트하는 검증 방법은
[SFTP WinFsp 마운트 기술 검증](docs/sftp-mount-spike.md)을 참고하세요.

## 알려진 제한 사항

- SFTP WinFsp 마운트는 기술 검증 단계이며 DKDrive 로컬 캐시 폴더를 사용
- 진행 중인 파일 전송 재개와 실패한 캐시 파일 복구 UI 미구현
- 읽기 캐시 만료, 용량 제한과 자동 정리 미구현
- WebDAV, FTP/FTPS 백엔드 미구현
- GUI와 트레이 미구현
- 보안 자격 증명 저장 미구현

개발 순서는 [로드맵](docs/roadmap.md)을 참고하세요.

## 라이선스

[MIT License](LICENSE)
