# DKDrive

**DKDrive (Direct Konnect Drive)**는 SFTP, WebDAV, FTP/FTPS 원격 저장소를
Windows 드라이브 문자로 마운트하는 경량 네트워크 드라이브 클라이언트입니다.

## 현재 상태

현재 버전은 `0.5.0-dev`입니다. WinFsp 메모리 드라이브와 실제 SFTP 드라이브의
연결·읽기·쓰기·이름 변경·이동·삭제·읽기 전용·자동 재연결을 Windows
탐색기와 메모장에서 검증했습니다. 기본 로컬 스테이징 캐시와 Windows 수정
시간·읽기 전용 속성 처리도 검증하여 0.2 SFTP 기본 범위를 완료했습니다.

0.3 WebDAV 백엔드는 HTTP/HTTPS Basic 인증부터 파일 작업, LOCK/UNLOCK,
읽기/쓰기 및 읽기 전용 WinFsp 마운트까지 Synology와 Windows 탐색기에서
검증했습니다.

0.4 FTP/FTPS 백엔드는 FTP, Explicit FTPS, Implicit FTPS의 비밀번호 인증,
원격 시작 경로, 목록, 읽기, 쓰기와 파일 작업을 모의 서버 자동 테스트로
검증했습니다. 일반 FTP와 Explicit FTPS는 Synology에서 CLI, WinFsp 기본 파일
작업, 읽기 전용 기본 동작, 탐색기·메모장과 제어 연결 단절 복구까지 확인했습니다.
Implicit FTPS 실서버 검증과 확장 장애·읽기 전용 검증은 아직 남아 있습니다.

0.5의 첫 Windows GUI, 복수 연결 관리, 트레이, 설정 저장, 선택적 DPAPI 자격
증명 저장과 자동 시작/자동 연결을 구현했습니다. FTPS/WebDAV GUI 복수 연결과
설정 복원·선택적 비밀번호 저장·프로그램 시작 시 자동 연결, Windows 자동 실행
등록·해제를 실환경에서 확인했습니다. 실제 재로그인 실행과 추가 GUI 검증은 남아
있습니다. 일반 해제 실패 후 사용자 승인으로 강제 해제하는 흐름도 개별 해제,
전체 해제의 혼합 선택과 프로그램 종료에서 실환경 검증했습니다. 기존 프로토콜별
CLI는 그대로 유지합니다.

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
go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
```

Windows에서 인수 없이 실행하면 연결 관리 GUI가 열립니다.

```powershell
.\bin\dkdrive.exe
```

프로필을 입력하고 **저장 → 선택 연결** 순으로 사용합니다. 비밀번호 저장과
Windows 로그인 시 실행, 연결별 자동 연결은 기본 해제되어 있습니다. 상세 동작과
수동 검증 순서는 [0.5 데스크톱 검증](docs/desktop-validation.md)을 참고하세요.
버전 확인은 `go run ./cmd/dkdrive --version`으로 할 수 있습니다.

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
- Implicit FTPS 실서버 검증 전; 장시간 단절·전송 중 복구 검증 전
- GUI 기본 검증과 WebDAV 쓰기는 완료; 실제 로그인 후 실행은 검증 대기
- 프로토콜별 GUI 전체 CRUD는 반복하지 않고 공통 GUI와 각 백엔드 검증 결과를 조합
- GUI 일반 해제 실패 시 강제 해제 확인 (기본: 취소); 미업로드 데이터 손실 가능성 안내
- 실패한 스테이징 파일 복구 UI는 아직 없으며, 캐시 보존 후 수동 복구 필요
- DPAPI 저장 비밀값은 현재 Windows 사용자/PC에 종속; 다른 PC로 복사해 재사용 불가
- GUI는 0.5 첫 화면이며 고급·캐시 탭은 후속 작업

개발 순서는 [로드맵](docs/roadmap.md)을 참고하세요.

## 라이선스

[MIT License](LICENSE)
