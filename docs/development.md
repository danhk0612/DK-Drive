# DK-Drive 개발 및 배포 안내

이 문서는 소스 빌드, 구현 상태, 검증 자료와 Windows 배포판 생성 절차를 다룬다.
일반 설치와 사용 방법은 [README](../README.md)와
[Windows 배포판 사용 안내](distribution.md)를 따른다.

## 기준 정보

- 제품 버전: 0.9.2
- EXE 파일 버전: 0.9.2.0
- 제작자: 참빛바다
- 라이선스: MIT
- 대상 환경: Windows 10/11 64비트
- 개발 도구: Go 1.26 이상
- 파일 시스템 런타임: WinFsp 2.1 이상
- 설치 관리자 생성: Inno Setup 6

`build-info.json`에는 빌드 버전, 소스 커밋, 로컬 수정 여부와 Go 버전을 기록한다.

## 빌드와 검사

저장소 루트의 PowerShell에서 실행한다.

```powershell
go test ./...
go vet ./...
go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
```

Windows에서 인수 없이 실행하면 연결 관리 GUI가 열린다.

```powershell
.\bin\dkdrive.exe
```

버전은 다음 명령으로 확인한다.

```powershell
go run ./cmd/dkdrive --version
```

## 구현 및 검증 상태

- SFTP: 비밀번호·개인키 인증, WinFsp 연결, 기본 파일 작업, 읽기 전용,
  자동 재연결, 로컬 staging과 Windows 수정 시간·읽기 전용 속성을 검증했다.
- WebDAV: HTTP/HTTPS Basic 인증, 기본 파일 작업, LOCK/UNLOCK, 읽기·쓰기 및
  읽기 전용 WinFsp 마운트를 Synology와 Windows 탐색기에서 검증했다.
- FTP/FTPS: FTP, Explicit FTPS, Implicit FTPS의 비밀번호 인증, 원격 시작 경로,
  목록·읽기·쓰기와 파일 작업을 자동 테스트로 검증했다. FTP와 Explicit FTPS는
  Synology에서 WinFsp 기본 파일 작업, 읽기 전용, 탐색기·메모장과 제어 연결 단절
  복구까지 확인했다. Implicit FTPS 실서버와 확장 장애 검증은 남아 있다.
- 데스크톱: 복수 연결, 트레이, 설정 저장, 선택적 DPAPI 자격 증명 저장,
  Windows 자동 실행, 자동 연결과 일반/강제 해제 흐름을 구현했다. 실제 로그인 후
  자동 실행과 일부 GUI·시스템 종료 검증은 남아 있다.
- VFS: 연결별 2초 메타데이터 캐시와 명확한 NotFound의 500ms 캐시를 적용했다.
  동시 조회 병합, 변경 시 무효화, 읽기 전용 조합과 실패 파일 보존을 자동 테스트 및
  WebDAV·Explicit FTPS 실환경 계측으로 검증했다.

세부 잔여 작업과 검증 경계는 [프로젝트 마무리 기준 상태](completion-status.md),
작업 순서는 [로드맵](roadmap.md), 데스크톱 제품화 범위는
[데스크톱 제품화 계획](desktop-product-plan.md)을 참고한다.

## 기술 문서와 검증 자료

- [아키텍처 결정](architecture.md)
- [WinFsp 메모리 마운트 기술 검증](winfsp-memory-spike.md)
- [SFTP 연결 기술 검증](sftp-connection-spike.md)
- [SFTP WinFsp 마운트 기술 검증](sftp-mount-spike.md)
- [WebDAV 연결 기술 검증](webdav-connection-spike.md)
- [Synology WebDAV 통합 검증 체크리스트](webdav-synology-validation.md)
- [FTP/FTPS 연결 기술 검증](ftp-connection-spike.md)
- [데스크톱 검증](desktop-validation.md)
- [메타데이터 캐시 검증](metadata-cache-validation.md)

## Windows 배포판 생성

저장소 루트의 PowerShell에서 실행한다. 버전은 앱의 `--version` 출력 한 곳에서
가져온다.

```powershell
.\scripts\package-windows.ps1
```

스크립트 실행에는 Inno Setup 6이 필요하다. 산출물은 `dist`의 설치형 EXE,
휴대용 ZIP과 각 `.sha256` 파일이다. ZIP에는 EXE, 공식 WinFsp MSI, 라이선스 모음,
사용 안내서, 빌드 정보와 EXE 체크섬을 포함한다. 설치형 EXE는 같은 파일을 내장하되
WinFsp MSI를 Program Files에 남기지 않는다.

같은 버전으로 다시 실행하면 기존 산출물과 체크섬을 교체한다. 재현 가능한 결과를
위해 같은 커밋, 깨끗한 작업 트리, Go 버전과 PowerShell 압축 도구 환경을 사용한다.
`-trimpath`로 로컬 경로를 제외하고 ZIP 항목 시간을 고정하지만, 다른 도구 버전 간
바이트 단위 동일성은 보장하지 않는다. CI는 압축 해제 후 EXE 체크섬을 검증한다.
공개 GitHub Release 생성과 업로드는 이 스크립트에서 수행하지 않는다.

## EXE 제품 정보 생성

앱 버전과 제작자는 `internal/app/app.go`에 정의한다. 버전을 변경하면 다음 명령으로
추적 중인 Windows amd64 리소스를 갱신하고 함께 커밋한다.

```powershell
go run ./internal/buildresources ./cmd/dkdrive/resource_windows_amd64.syso
```

일반 `go build`와 패키징은 이 리소스를 포함한다. CI에서 리소스를 다시 생성해
소스와 일치하는지 확인하고, 최종 EXE의 제품명·제작자·문자열/숫자 버전을 검증한다.
아이콘은 기존 창·트레이와 같은 도형을 16/32/48픽셀로 포함한다.

## WinFsp 배포와 라이선스

DK-Drive는 MIT 라이선스를 유지하고 WinFsp FLOSS 예외에 따라 공식 WinFsp MSI
원본을 배포판에 포함한다. 패키징 스크립트는 `internal/prerequisite/winfsp.json`에
고정된 다운로드 주소와 SHA-256으로 MSI를 가져와 검증한다. 설치 프로그램은 WinFsp가
없는 경우에만 설치하며, DK-Drive 제거 시 공유 런타임인 WinFsp는 남긴다.


---
WinFsp - Windows File System Proxy, Copyright (C) Bill Zissimopoulos

[WinFsp 저장소](https://github.com/winfsp/winfsp) · GPLv3 + FLOSS 예외 적용. DK-Drive 자체는 MIT.
