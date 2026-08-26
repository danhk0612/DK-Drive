# SFTP WinFsp 마운트 기술 검증

이 단계는 SFTP 원격 경로를 WinFsp 드라이브 문자로 마운트하여 Windows
탐색기에서 기본 파일 작업을 검증한다. 실제 데이터가 아닌 테스트 전용
경로를 원격 시작 경로로 사용하는 것을 권장한다.

## 빌드와 실행

```powershell
go build -o bin/dkdrive-sftp-mount.exe ./cmd/dkdrive-sftp-mount
.\bin\dkdrive-sftp-mount.exe `
    -host example.com `
    -port 22 `
    -user tester `
    -root /home/tester/dkdrive-test `
    -mount X:
```

등록된 `known_hosts` 파일로 호스트 키를 검증하며 비밀번호는 화면에 표시하지
않고 입력받는다. 마운트를 해제하려면 실행 중인 창에서 `Ctrl+C`를 누른다.

개인키 인증은 비밀번호 대신 `-key "$HOME\.ssh\id_ed25519"`를 지정한다.
암호화된 개인키는 Passphrase를 화면에 표시하지 않고 입력받는다.

## 읽기 전용 마운트

`-read-only`를 지정하면 파일과 폴더의 생성·쓰기·삭제·이름 변경·이동·수정
시간 변경을 VFS 계층에서 차단하고 Windows에는 읽기 전용 속성으로 표시한다.

```powershell
.\bin\dkdrive-sftp-mount.exe `
    -host example.com -port 22 -user tester -root /data `
    -key "$HOME\.ssh\id_ed25519" -read-only -mount X:
```

## 1차 점검 순서

마운트 후 별도 PowerShell 창에서 다음과 같이 격리된 폴더를 사용한다.

```powershell
New-Item 'X:\DKDrive 마운트 테스트' -ItemType Directory
Set-Content 'X:\DKDrive 마운트 테스트\hello world.txt' 'DKDrive 테스트'
Get-Content 'X:\DKDrive 마운트 테스트\hello world.txt'
Rename-Item 'X:\DKDrive 마운트 테스트\hello world.txt' '이름 변경.txt'
Move-Item 'X:\DKDrive 마운트 테스트\이름 변경.txt' 'X:\이동 테스트.txt'
Get-Content 'X:\이동 테스트.txt'
Remove-Item 'X:\이동 테스트.txt'
Remove-Item 'X:\DKDrive 마운트 테스트'
```

Windows 탐색기에서도 폴더 목록, 파일 생성, 메모장 저장, 이름 변경, 이동,
삭제를 확인한다.

## 현재 제한 사항

- 열린 파일은 시스템 임시 폴더에 준비한 뒤 WinFsp `Flush` 또는 파일 닫기
  시점에 SFTP로 반영한다.
- 반영 실패 시 데이터 보존을 위해 임시 파일을 삭제하지 않지만 복구 UI는
  아직 없다.
- 네트워크 재연결, 캐시 관리와 충돌 처리는 아직 구현하지 않았다.
- 쓰기 권한이 없는 원격 시작 경로에서는 조회만 가능하고 파일 작업은 실패한다.
- 현재 SFTP 서버의 경로 규칙에 맞춰 파일명의 대소문자를 구분한다.

## 검증 결과

- 날짜: 2026-08-26
- 통과: SFTP 원격 경로를 WinFsp 드라이브 문자로 마운트
- 통과: 한글·공백 경로의 폴더와 파일 생성
- 통과: 작성한 파일 재읽기
- 통과: 이름 변경과 다른 폴더로 이동
- 통과: 이동한 파일 재읽기
- 통과: 파일과 폴더 삭제 후 경로가 남지 않음
- 통과: Windows 탐색기에서 폴더 생성·이름 변경·이동·삭제
- 통과: 메모장에서 한글 파일 생성·저장·종료 후 다시 열기
- 통과: 읽기 전용 마운트에서 조회와 파일 읽기
- 통과: 파일·폴더 생성, 쓰기, 이름 변경, 이동, 삭제 차단
- 통과: 쓰기용 파일 열기를 즉시 액세스 거부로 반환하며 원격 파일이 생성되지 않음
- 확인: 전체 작업 중 마운트 실행 창에 오류 없음

초기에는 SFTP 백엔드를 대소문자 비구분 파일시스템으로 잘못 등록하여 이름
변경 시 원본 경로를 찾지 못했다. WinFsp 설정을 실제 SFTP 경로 규칙에 맞춘
후 동일한 작업 순서가 정상 동작했다.

이 결과로 0.1 SFTP–WinFsp 기술 검증을 완료한다.
