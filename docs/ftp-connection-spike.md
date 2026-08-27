# FTP/FTPS 연결 기술 검증

0.4는 FTP, Explicit FTPS와 Implicit FTPS의 비밀번호 인증, 원격 시작 경로,
파일 작업과 WinFsp 마운트를 검증한다. 자동 테스트를 통과한 기능도 실제 서버와
Windows에서 확인하기 전에는 로드맵을 완료 처리하지 않는다.

## 자동 테스트 범위

- 일반 FTP 연결과 로그인
- `AUTH TLS`를 사용하는 Explicit FTPS
- 처음부터 TLS로 연결하는 Implicit FTPS
- `CWD`와 `PWD`를 사용한 원격 시작 경로 확인
- UTF-8 파일명, `MLSD` 목록과 `MLST` 속성
- 수동형 데이터 연결의 `RETR` 파일 읽기
- 로컬 임시 파일을 사용한 임의 위치 쓰기와 `STOR` 전체 파일 반영
- `MKD`, `RNFR`/`RNTO`, `DELE`, `RMD` 파일 작업
- 서버가 광고하는 경우 `MFMT` 수정 시간 설정
- 연결 전체 읽기 전용과 WinFsp 마운트 Windows 빌드

`github.com/jlaffaye/ftp`는 연결 하나에서 동시에 여러 명령을 처리할 수 없으므로
백엔드는 목록, 속성 조회와 파일 전송을 직렬화한다.

## 빌드

```powershell
go test ./...
go vet ./...
go build -o bin/dkdrive-ftp.exe ./cmd/dkdrive-ftp
go build -o bin/dkdrive-ftp-mount.exe ./cmd/dkdrive-ftp-mount
```

## FTP

FTP는 인증 정보와 파일 내용이 암호화되지 않는다. 신뢰할 수 있는 내부망에서만
기술 검증 용도로 사용한다.

```powershell
.\bin\dkdrive-ftp.exe `
    -mode ftp `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot
```

## Explicit FTPS

기본 포트는 21이며 평문 연결 후 `AUTH TLS`로 제어 연결과 데이터 연결을
암호화한다.

```powershell
.\bin\dkdrive-ftp.exe `
    -mode explicit-ftps `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot
```

## Implicit FTPS

기본 포트는 990이며 최초 연결부터 TLS를 사용한다.

```powershell
.\bin\dkdrive-ftp.exe `
    -mode implicit-ftps `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot
```

서버 인증서가 IP 주소와 일치하지 않는 내부 테스트 서버인 경우에만 다음 옵션을
명시적으로 추가한다.

```powershell
-insecure-skip-tls-verify
```

이 옵션은 인증서의 신원과 신뢰 체인 검증을 모두 건너뛰므로 외부망에서는
사용하지 않는다.

## 파일 읽기

목록 조회가 성공하면 출력에 포함된 기존 파일 하나를 지정한다.

```powershell
.\bin\dkdrive-ftp.exe `
    -mode explicit-ftps `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot `
    -read '기존 파일.txt'
```

비밀번호는 실행 중 콘솔에서 가려진 상태로 입력한다. 자동 테스트에서는
`DKDRIVE_FTP_PASSWORD` 환경 변수를 사용할 수 있지만 실제 비밀번호를 저장소나
일반 설정 파일에 기록하지 않는다.

## 기본 쓰기

연결과 읽기가 통과한 동일한 모드에서 격리된 임시 경로의 생성, 쓰기, 읽기,
이동, 이름 변경과 삭제를 순서대로 검증한다.

```powershell
.\bin\dkdrive-ftp.exe `
    -mode explicit-ftps `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot `
    -write-test
```

## WinFsp 마운트

실서버 기본 쓰기까지 통과한 모드로 마운트한다.

```powershell
.\bin\dkdrive-ftp-mount.exe `
    -mode explicit-ftps `
    -host $FTPHost `
    -port $FTPPort `
    -user $FTPUser `
    -root $FTPRoot `
    -mount 'X:'
```

읽기 전용 검증에는 같은 명령 끝에 `-read-only`를 추가한다. 인증서 검증을
우회해야 하는 신뢰할 수 있는 내부 테스트 서버인 경우에만
`-insecure-skip-tls-verify`를 추가한다.

## 현재 범위

- 연결, 목록, 읽기, 쓰기, 파일 작업과 WinFsp 마운트 코드 구현 완료
- FTP/FTPS 실제 서버 및 Windows 탐색기 검증 전
- 수정 시간은 서버가 `MFMT` 또는 쓰기 가능한 `MDTM`을 광고할 때만 지원
- 파일별 Windows ReadOnly 속성 변경 미지원
- 자동 재연결 미구현
- 서버별 `MLSD`, `MLST`, UTF-8 및 수동형 연결 차이 검증 전
