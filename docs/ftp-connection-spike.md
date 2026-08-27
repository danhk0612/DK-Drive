# FTP/FTPS 연결 기술 검증

0.4는 FTP, Explicit FTPS와 Implicit FTPS의 비밀번호 인증, 원격 시작 경로,
파일 작업과 WinFsp 마운트를 검증한다. 자동 테스트를 통과한 기능도 실제 서버와
Windows에서 확인하기 전에는 로드맵을 완료 처리하지 않는다.

## 2026-08-27 Synology Explicit FTPS 검증 상태

- [x] `192.168.0.150:4021` 연결과 `AUTH TLS`
- [x] 비밀번호 인증
- [x] `/home/` 원격 시작 경로 확인
- [x] 수동형 TLS 데이터 연결
- [x] `MLSD` 디렉터리 목록, 크기와 수정 시간
- [x] UTF-8 및 한글 파일명
- [x] 기존 한글 파일 `RETR` 읽기와 내용 일치
- [x] IP SAN이 없는 내부 NAS 인증서에서 명시적 TLS 검증 우회 확인
- [x] CLI `-write-test`: 파일·폴더 생성, 쓰기, 읽기 내용 검증, 이동, 이름 변경, 삭제
- [x] Explicit FTPS 읽기/쓰기 WinFsp 마운트와 PowerShell 기본 파일 작업
- [x] 읽기 전용 마운트 기본 검증: 목록·내용 읽기, ReadOnly 표시, 새 파일 생성 차단

일반 FTP·Implicit FTPS·자동 재연결·탐색기 UI 검증은 별도로 남긴다.

### 업로드 후 제어 연결 단절 진단

실서버 `-write-test`는 폴더 생성과 업로드 뒤, 같은 연결에서 파일을 다시 읽는
단계에 제어 연결 오류가 발생했다. 새 CLI 프로세스로 확인한 결과 원본 파일은
26바이트이며 내용도 일치했다. 업로드 데이터는 확인됐지만 제어 연결 종료 원인은
아직 확정하지 않았다. 당시 이동·이름 변경·삭제까지의 쓰기 검증은 완료하지 못했다.

전송 뒤 다음 작업 전에 `NOOP`로 제어 연결을 확인하고, 연결 단절이면 같은
인증·TLS 설정과 시작 경로로 재연결한다. 조회·목록·읽기 시작은 연결 오류에 한해
한 번 재시도한다. 이미 반환한 읽기 스트림은 자동으로 이어받지 않는다.
변경 명령과 업로드의 실패는 서버 반영 여부가 불확실하므로 연결 복구 로직에서
재전송하지 않고 호출자에게 오류를 반환한다. 시작 경로가 달라지면 작업을 중단한다.

모의 서버에서 FTP·Explicit FTPS·Implicit FTPS의 업로드 후 단절과 복구,
읽기 내용 일치, 변경 명령의 중복 전송 방지를 확인한다.

`48cbb94`를 적용하고 두 FTP 실행 파일을 재빌드한 뒤, 원래 시작 경로에서
`-write-test`를 재실행했다. 2026-08-27 15:06:14에 시작한 테스트는 생성,
쓰기, 읽기 내용 검증, 이동, 이름 변경, 삭제까지 모두 통과했다.
이 결과는 CLI 쓰기 시나리오의 재검증 통과이며, 출력에 재연결 이력이 없으므로
실제로 재연결이 발생했는지 또는 최초 연결 종료 원인이 무엇인지는 확정하지 않는다.
자동 재연결과 장시간 단절 검증은 별도로 남긴다. 기존 실패 폴더는 보존하고,
다음 단계로 Explicit FTPS 읽기/쓰기 WinFsp 마운트를 검증한다.

### WinFsp 쓰기 직후 조회 진단

읽기/쓰기 마운트와 기존 파일의 속성·내용 조회는 통과했다. 새 폴더 생성 뒤
`Set-Content`는 오류 없이 끝났지만, 바로 이어진 `Get-Content`는 파일 없음으로
실패했다. 이후 마운트와 별도 CLI 연결에서 38바이트 파일 및 작성 내용이 확인됐다.
별도 진단 파일에서 `FileStream.Flush(true)` 후 즉시 읽기·내용 비교는 통과했다.

공통 마운트 계층에서 같은 경로의 미반영 스테이징 쓰기를 먼저 동기화한 뒤
원격 속성을 조회하도록 수정했다. `Stat`과 읽기용 `OpenFile` 모두 이 경로를
사용하며, 다른 경로의 대기 중인 파일은 동기화하지 않는다. 쓰기·잘라내기·동기화·
닫기를 파일별 잠금으로 직렬화한다. 업로드 실패는 조회 호출자에게 반환하고
기존 임시 파일 보존 정책을 유지한다.

`10b70d1` 적용 후 2026-08-27 15:24:03에 새 파일로 재검증했다.
명시적 Flush나 대기 없이 `Set-Content` 직후 `Get-Content`가 성공했고,
작성 문자열과 읽은 문자열의 일치 검증도 통과했다.

이어서 같은 재검증 파일의 이름 변경, 새 하위 폴더 생성, 폴더 간 이동,
이동 후 내용 일치, 파일·폴더 삭제 및 삭제 후 경로 부재 확인까지 모두 통과했다.
읽기/쓰기 WinFsp 마운트의 PowerShell 기본 파일 작업은 완료로 기록한다.
이번 재검증 파일과 이동용 하위 폴더만 삭제했으며, 기존 진단 폴더와 다른 파일은
유지했다. 이후 읽기 전용 마운트 기본 검증은 아래와 같이 통과했으며,
자동 재연결·탐색기 UI 검증은 아직 미완료다.

### 읽기 전용 마운트 기본 검증

`-read-only`로 재마운트한 뒤 디렉터리 목록, 기존 파일의 18바이트 크기와
`ReadOnly` 속성, 기존 내용 읽기를 확인했다. 2026-08-27 15:35:09의
새 파일 생성 시도는 `PermissionDenied`로 차단됐고, `Test-Path`는 `False`였다.

이 결과는 읽기 유지와 새 파일 생성 차단의 실서버 기본 검증이다. 기존 파일의
덮어쓰기·이름 변경·삭제 차단을 실제로 시도한 결과는 아직 없으며, 이번 검증으로
해당 항목까지 통과했다고 간주하지 않는다. 기존 진단 파일은 변경하지 않았다.

### 읽기 전용 탐색기 UI 검증

읽기 전용 마운트를 유지한 상태에서 탐색기의 파일·폴더 목록 표시,
폴더 진입과 상위 폴더 이동, 기존 텍스트 파일 열기가 모두 정상임을 확인했다.
사용자는 오류나 멈춤 없이 동작한다고 보고했다. 기존 파일은 변경하지 않았다.

탐색기의 읽기/쓰기 모드에서 생성·저장·이름 변경·이동·삭제는 아직 검증 전이므로
로드맵의 탐색기 실제 사용 점검 전체 항목은 완료 처리하지 않는다.

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
- Explicit FTPS CLI 및 읽기/쓰기 WinFsp 마운트의 PowerShell 기본 파일 작업 실서버 검증 완료
- 읽기 전용 마운트 기본 검증 완료; 기존 파일 덮어쓰기·이름 변경·삭제 차단 실환경 검증 전
- 읽기 전용 탐색기 목록·폴더 이동·텍스트 열기 검증 완료
- 자동 재연결·탐색기 읽기/쓰기 UI 검증 미완료
- 수정 시간은 서버가 `MFMT` 또는 쓰기 가능한 `MDTM`을 광고할 때만 지원
- 파일별 Windows ReadOnly 속성 변경 미지원
- 전송 후 연결 확인 및 제한적인 재연결 구현; 실서버 복구·장시간 단절 검증 전
- 서버별 `MLSD`, `MLST`, UTF-8 및 수동형 연결 차이 검증 전
