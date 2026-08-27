# Synology WebDAV 통합 검증 체크리스트

이 문서는 0.3 WebDAV 자동 테스트 이후 실제 Synology NAS와 Windows 10/11에서 수행할 검증 순서를 정리한다. 앞 단계가 실패하면 다음 단계로 넘어가지 않고 전체 오류를 기록한다.

## 1. 코드와 실행 파일 준비

```powershell
Set-Location 'D:\DK-Drive\DK-Drive'

git pull
git rev-parse HEAD

go test ./...
go vet ./...
go build -o bin/dkdrive-webdav.exe ./cmd/dkdrive-webdav
go build -o bin/dkdrive-webdav-mount.exe ./cmd/dkdrive-webdav-mount
```

예상 기준 커밋은 이 문서를 추가한 최신 `main` 커밋이다.

## 2. HTTPS 연결 변수

`$WebDAVHost`에는 가능하면 Synology WebDAV 인증서에 포함된 호스트 이름을 사용한다. 현재처럼 IP 주소를 사용해 인증서 이름 불일치가 발생하면, 신뢰할 수 있는 내부 NAS를 직접 지정한 경우에만 `$WebDAVSkipTLSVerify`를 `$true`로 둔다. 이 옵션은 인증서의 신원과 신뢰 체인 검증을 모두 건너뛰므로 외부망에서는 사용하지 않는다.

```powershell
$WebDAVScheme = 'https'
$WebDAVHost = '192.168.0.150'
$WebDAVPort = 5006
$WebDAVUser = 'danhk0612'
$WebDAVRoot = '/home/'
$WebDAVSkipTLSVerify = $true
```

## 3. 목록과 읽기

2026-08-27 Synology 실서버 검증 상태:

- [x] HTTPS 5006 연결 및 Basic 인증
- [x] `/home/` 원격 시작 경로 확인
- [x] PROPFIND 디렉터리 목록
- [x] 한글 파일명과 서버 수정 시간 해석
- [x] 기존 한글 파일 GET 읽기와 내용 일치 확인

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify"
```

목록이 정상 출력되면 기존 파일을 읽는다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -read 'my.cnf'
```

## 4. 서버 기능 조회

2026-08-27 Synology 실서버 OPTIONS 결과:

- DAV 클래스: `1`, `2`, `<http://apache.org/dav/propset/fs/1>`
- 허용 메서드: `COPY`, `DELETE`, `GET`, `HEAD`, `LOCK`, `MOVE`, `OPTIONS`, `POST`, `PROPFIND`, `PROPPATCH`, `PUT`, `TRACE`, `UNLOCK`
- DAV 클래스 2와 `LOCK`/`UNLOCK`이 확인되어 잠금 검증 가능

이 단계는 원격 파일을 변경하지 않는다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -capabilities
```

출력된 DAV 클래스와 허용 메서드 전체를 기록한다.

## 5. 기본 쓰기

격리된 임시 폴더를 만들고 `MKCOL`, `PUT`, `GET`, `MOVE`, `DELETE`를 순서대로 검증한 뒤 삭제한다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -write-test
```

## 6. LOCK과 UNLOCK

4단계 결과에 DAV 클래스 `2`와 `LOCK`, `UNLOCK`이 포함된 경우에만 실행한다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -lock-test
```

## 7. 읽기/쓰기 마운트

별도 PowerShell 창에서 마운트를 실행하고 비밀번호를 입력한다.

```powershell
.\bin\dkdrive-webdav-mount.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -mount 'X:'
```

다른 PowerShell 창에서 한글과 공백을 포함한 기본 파일 작업을 검증한다.

```powershell
$TestDir = 'X:\DKDrive WebDAV 마운트 테스트'
$TestFile = "$TestDir\한글 파일.txt"
$MovedFile = 'X:\DKDrive WebDAV 이동 테스트.txt'

New-Item $TestDir -ItemType Directory -ErrorAction Stop | Out-Null
Set-Content $TestFile 'DKDrive WebDAV 테스트' -Encoding UTF8 -ErrorAction Stop
Get-Content $TestFile -Encoding UTF8 -ErrorAction Stop
Rename-Item $TestFile '이름 변경.txt' -ErrorAction Stop
Move-Item "$TestDir\이름 변경.txt" $MovedFile -ErrorAction Stop
Get-Content $MovedFile -Encoding UTF8 -ErrorAction Stop
Remove-Item $MovedFile -ErrorAction Stop
Remove-Item $TestDir -ErrorAction Stop
Test-Path $MovedFile
Test-Path $TestDir
```

두 `Test-Path` 결과는 모두 `False`여야 한다. 마운트 창에서 `Ctrl+C`를 눌러 해제한다.

## 8. 연결 전체 읽기 전용

읽기 전용으로 다시 마운트한다.

```powershell
.\bin\dkdrive-webdav-mount.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    "-insecure-skip-tls-verify=$WebDAVSkipTLSVerify" `
    -mount 'X:' `
    -read-only
```

다른 창에서 목록과 기존 파일 읽기는 성공하고 생성은 거부되는지 확인한다.

```powershell
Get-ChildItem 'X:\' -Force -ErrorAction Stop
Get-Content 'X:\my.cnf' -ErrorAction Stop
Set-Content 'X:\읽기 전용 생성 차단.txt' '생성되면 안 됨' -ErrorAction Stop
Test-Path 'X:\읽기 전용 생성 차단.txt'
```

`Set-Content`는 권한 오류가 나야 하고 `Test-Path`는 `False`여야 한다.

## 9. HTTP 선택 검증

HTTP 지원은 신뢰할 수 있는 내부망에서만 별도로 확인한다. Basic 인증 자격 증명이 암호화되지 않으므로 외부망에서는 실행하지 않는다.

```powershell
$WebDAVScheme = 'http'
$WebDAVPort = 5005

.\bin\dkdrive-webdav.exe `
    -scheme $WebDAVScheme `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot
```

## 현재 의도된 제한

- WebDAV 수정 시간 설정 미지원
- 파일별 Windows ReadOnly 속성 변경 미지원
- 사용자 지정 CA 인증서 파일 미지원
- `-insecure-skip-tls-verify`는 명시적으로 지정한 경우에만 인증서 검증을 우회
- 중단된 HTTP 전송 이어받기 미지원
