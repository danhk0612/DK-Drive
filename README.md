# DK-Drive

**DK-Drive (Direct Konnect Drive)**는 SFTP, WebDAV, FTP/FTPS 원격 저장소를
Windows 탐색기에서 일반 드라이브처럼 사용할 수 있게 연결하는 프로그램입니다.

현재 버전은 **0.9.2**, 제작자는 **참빛바다**입니다. Windows 10/11 64비트를
지원하며, 정식 1.0 전 공개 검증 버전입니다.

## 다운로드와 설치

- [최신 릴리스 다운로드](https://github.com/danhk0612/DK-Drive/releases/latest)
- 일반 사용자는 `DK-Drive-0.9.2-windows-amd64-setup.exe`를 권장합니다.
- 설치 없이 사용하려면 `DK-Drive-0.9.2-windows-amd64.zip`을 내려받아 전체 압축을 풉니다.

설치형 EXE에는 공식 WinFsp 설치 파일이 포함되어 있습니다. WinFsp가 없는 PC에서는
설치 중 관리자 승인을 받아 함께 설치하며, 이미 설치되어 있으면 이 과정을 건너뜁니다.
휴대용 ZIP에도 같은 설치 파일이 포함되며, WinFsp가 없으면 실행 시 설치를 안내합니다.
설치 프로그램과 실행 파일은 아직 코드 서명되지 않았으므로 Windows 경고가 표시될 수
있습니다.

## 주요 기능

- SFTP 비밀번호 및 개인키 인증
- HTTP/HTTPS WebDAV
- FTP, Explicit FTPS, Implicit FTPS
- 여러 연결 프로필과 드라이브 문자 관리
- 연결별 읽기 전용, 자동 연결, 자동 재연결
- Windows 탐색기에서 읽기·쓰기·이름 변경·이동·삭제
- 서버 측 파일 정보 단기 캐시를 통한 탐색 성능 개선
- 실패한 쓰기 파일 보존, 내보내기 및 원격 재시도
- 트레이 상주와 선택적 Windows 로그인 시 실행

## 처음 연결하기

1. 설치 후 시작 메뉴에서 **DK-Drive**를 실행합니다.
2. **드라이브 추가**를 눌러 프로토콜과 서버 주소, 계정, 원격 시작 경로,
   사용할 드라이브 문자를 입력합니다.
3. 필요하면 읽기 전용, 자동 연결, 비밀번호 저장을 선택합니다.
4. **저장**한 뒤 목록에서 프로필을 선택하고 **선택 연결**을 누릅니다.
5. 작업을 마치면 열린 파일을 닫고 **선택 해제**한 뒤 트레이 메뉴에서 종료합니다.

비밀번호 저장, Windows 로그인 시 실행, 자동 연결은 기본적으로 꺼져 있습니다.
일반 FTP는 비밀번호와 데이터가 암호화되지 않으므로 가능한 경우 SFTP, HTTPS WebDAV
또는 FTPS를 사용하세요.

## 업데이트와 제거

업데이트 전 열린 원격 파일을 저장하고 연결을 정상 해제한 뒤 DK-Drive를 완전히
종료합니다. 새 설치형 EXE를 실행하면 기존 설정과 복구 캐시는 유지됩니다.

제거는 Windows **설치된 앱**에서 DK-Drive를 선택합니다. 다른 프로그램도 WinFsp를
사용할 수 있으므로 DK-Drive 제거 시 WinFsp는 자동 제거하지 않습니다. 설정과 복구
파일도 자동 삭제하지 않습니다.

- 설정: `%APPDATA%\DKDrive\settings.json`
- 복구 캐시: `%LOCALAPPDATA%\DKDrive\Cache`

## 문제 해결과 복구

- 드라이브가 연결되지 않으면 WinFsp 설치 여부와 선택한 드라이브 문자의 중복을
  확인하고, WinFsp 설치 직후라면 Windows를 재시작합니다.
- 일반 해제가 실패했을 때 강제 해제하면 아직 원격 서버에 반영되지 않은 데이터가
  남을 수 있습니다. 가능한 한 일반 해제를 먼저 사용합니다.
- 업로드에 실패한 파일은 **캐시 관리**에서 먼저 로컬로 내보낸 뒤, 연결이 복구되면
  원격 재시도를 사용합니다. 충돌 안내가 나오면 원격 파일과 보존본을 확인한 뒤
  처리합니다.
- 복구가 끝나기 전에는 캐시 전체 정리를 사용하지 마세요.

## 현재 제한 사항

- 코드 서명과 자동 업데이트는 아직 제공하지 않습니다.
- 중단된 파일 전송을 중단 지점부터 이어받지는 않습니다.
- 파일 내용 읽기 캐시는 없으며 파일 정보는 최대 2초간 재사용하므로 외부 변경은
  새로고침 후 보일 수 있습니다.
- Implicit FTPS 실서버, 장시간·전송 중 단절, Windows 로그인·종료의 일부 실환경
  검증이 남아 있습니다.
- 저장한 비밀값은 현재 Windows 사용자와 PC에 종속되어 다른 PC에서 재사용할 수
  없습니다.

## 문서

- [Windows 배포판 사용 안내](docs/distribution.md)
- [개발 및 배포 안내](docs/development.md)
- [릴리스 내역](https://github.com/danhk0612/DK-Drive/releases)

## 라이선스

[MIT License](LICENSE)


---
WinFsp - Windows File System Proxy, Copyright (C) Bill Zissimopoulos

[WinFsp 저장소](https://github.com/winfsp/winfsp) · GPLv3 + FLOSS 예외 적용. DK-Drive 자체는 MIT.
