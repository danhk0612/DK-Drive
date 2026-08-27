# DKDrive

**DKDrive (Direct Konnect Drive)**는 SFTP, WebDAV, FTP/FTPS 원격 저장소를
Windows 드라이브 문자로 마운트하는 경량 네트워크 드라이브 클라이언트입니다.

## 현재 상태

현재 버전은 `0.4.0-dev`입니다. WinFsp 메모리 드라이브와 실제 SFTP 드라이브의
연결·읽기·쓰기·이름 변경·이동·삭제·읽기 전용·자동 재연결을 Windows
탐색기와 메모장에서 검증했습니다. 기본 로컬 스테이징 캐시와 Windows 수정
시간·읽기 전용 속성 처리도 검증하여 0.2 SFTP 기본 범위를 완료했습니다.

0.3 WebDAV 백엔드는 HTTP/HTTPS Basic 인증부터 파일 작업, LOCK/UNLOCK,
읽기/쓰기 및 읽기 전용 WinFsp 마운트까지 Synology와 Windows 탐색기에서
검증했습니다.

0.4 FTP/FTPS 백엔드는 FTP, Explicit FTPS, Implicit FTPS의 비밀번호 인증,
원격 시작 경로, 목록, 읽기, 쓰기와 파일 작업을 모의 서버 자동 테스트로
검증했습니다. WinFsp 마운트도 구현했으며 실제 서버와 Windows 검증 전입니다.

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

WebDAV 연결, 파일 작업과 WinFsp 마운트 검증 방법은
[WebDAV 연결 기술 검증](docs/webdav-connection-spike.md)을 참고하세요.
실제 Synology와 Windows에서 순차 점검할 명령은
[Synology WebDAV 통합 검증 체크리스트](docs/webdav-synology-validation.md)에 정리했습니다.

FTP와 FTPS 연결 및 읽기 검증 방법은
[FTP/FTPS 연결 기술 검증](docs/ftp-connection-spike.md)을 참고하세요.

## 알려진 제한 사항

- SFTP WinFsp 마운트는 기술 검증 단계이며 DKDrive 로컬 캐시 폴더를 사용
- 진행 중인 파일 전송 재개와 실패한 캐시 파일 복구 UI 미구현
- 읽기 캐시 만료, 용량 제한과 자동 정리 미구현
- SFTP에는 별도 생성·접근 시간이 없어 Windows의 생성·접근 시간에 수정 시간을 표시
- Hidden, System, Archive 등 SFTP에 대응값이 없는 Windows 속성 변경 미지원
- WebDAV 수정 시간과 파일별 ReadOnly 속성 변경 미지원
- FTP/FTPS 백엔드와 WinFsp 마운트는 자동 테스트만 완료했으며 실제 서버 검증 전
- FTP/FTPS 자동 재연결 미구현
- GUI와 트레이 미구현
- 보안 자격 증명 저장 미구현

개발 순서는 [로드맵](docs/roadmap.md)을 참고하세요.

## 라이선스

[MIT License](LICENSE)
