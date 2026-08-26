# DKDrive

**DKDrive (Direct Konnect Drive)**는 SFTP, WebDAV, FTP/FTPS 원격 저장소를
Windows 드라이브 문자로 마운트하는 경량 네트워크 드라이브 클라이언트입니다.

## 현재 상태

현재 버전은 `0.1.0-dev`이며 기술 검증 단계입니다. WinFsp 메모리 드라이브와
SFTP 연결·읽기·쓰기 검증을 통과했으며, 실제 SFTP 드라이브 마운트 스파이크를
검증하고 있습니다. Windows 탐색기의 파일 읽기·쓰기 안정성을 우선합니다.

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
- WinFsp(마운트 기능 구현 후 런타임으로 필요)

WinFsp 설치와 정확한 빌드 절차는 0.1 기술 검증이 끝난 뒤 확정합니다.

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

- SFTP WinFsp 마운트는 기술 검증 단계이며 로컬 임시 파일을 사용
- 네트워크 장애 복구와 실패한 임시 파일 관리 미구현
- WebDAV, FTP/FTPS 백엔드 미구현
- GUI와 트레이 미구현
- 캐시와 보안 자격 증명 저장 미구현

개발 순서는 [로드맵](docs/roadmap.md)을 참고하세요.

## 라이선스

[MIT License](LICENSE)
