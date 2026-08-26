# WebDAV 연결 기술 검증

## 현재 범위

- HTTP 및 HTTPS
- Basic 사용자명/비밀번호 인증
- 호스트, 포트, 원격 시작 경로
- `PROPFIND` Depth 0으로 시작 경로 확인
- `PROPFIND` Depth 1로 디렉터리 목록 조회
- `GET`으로 파일 읽기
- 한글과 공백이 포함된 URL 경로 인코딩

코드와 모의 서버 자동 테스트에는 `MKCOL`, `PUT`, `MOVE`, `DELETE`가 구현되어 있다. Synology 실서버 동작은 아직 검증하지 않았다. 선택 기능인 `LOCK`, `UNLOCK`은 기본 파일 작업 검증 후 지원 여부를 확인한다.

## 보안 기준

Basic 인증 자체는 자격 증명을 암호화하지 않는다. 외부 네트워크에서는 신뢰할 수 있는 인증서가 설정된 HTTPS를 사용한다. 현재 구현은 운영체제 인증서 저장소로 서버 인증서를 검증하며 인증서 검증 우회 옵션을 제공하지 않는다.

## Synology 검증 절차

Synology WebDAV Server의 기본 포트는 HTTP 5005, HTTPS 5006이며 설정에서 변경할 수 있다. 아래 예시는 HTTPS 5006을 사용한다.

```powershell
go build -o bin/dkdrive-webdav.exe ./cmd/dkdrive-webdav

$WebDAVHost = '192.168.0.150'
$WebDAVPort = 5006
$WebDAVUser = 'danhk0612'
$WebDAVRoot = '/home/'

.\bin\dkdrive-webdav.exe `
    -scheme https `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot
```

목록이 확인되면 실제 존재하는 파일을 읽는다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme https `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    -read 'my.cnf'
```

서버가 광고하는 DAV 클래스와 허용 메서드도 조회한다. 이 명령은 원격 파일을 변경하지 않는다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme https `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    -capabilities
```

마지막으로 격리된 임시 폴더에서 생성, 쓰기, 읽기, 이동, 이름 변경, 삭제를 검증한다.

```powershell
.\bin\dkdrive-webdav.exe `
    -scheme https `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    -write-test
```

IP 주소와 인증서 이름이 일치하지 않으면 HTTPS 인증서 검증이 실패할 수 있다. 이 경우 인증서에 포함된 NAS 호스트 이름을 `-host`에 사용한다.

## WinFsp 마운트 검증

연결과 기본 쓰기 검증이 모두 통과한 뒤 마운트 실행 파일을 빌드한다.

```powershell
go build -o bin/dkdrive-webdav-mount.exe ./cmd/dkdrive-webdav-mount

.\bin\dkdrive-webdav-mount.exe `
    -scheme https `
    -host $WebDAVHost `
    -port $WebDAVPort `
    -user $WebDAVUser `
    -root $WebDAVRoot `
    -mount 'X:'
```

현재 WebDAV 마운트의 수정 시간 설정과 파일별 ReadOnly 속성 변경은 구현되지 않았다. 서버별 지원 범위를 확인하기 전에는 해당 속성 변경을 테스트하지 않는다.

연결 전체를 읽기 전용으로 마운트할 때는 `-read-only`를 추가한다.
