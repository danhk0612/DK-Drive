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
- 네트워크 재연결, 읽기 전용, 캐시 관리와 충돌 처리는 아직 구현하지 않았다.
- 쓰기 권한이 없는 원격 시작 경로에서는 조회만 가능하고 파일 작업은 실패한다.
- 현재 SFTP 서버의 경로 규칙에 맞춰 파일명의 대소문자를 구분한다.
