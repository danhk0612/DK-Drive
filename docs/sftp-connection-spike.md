# SFTP 연결 기술 검증

이 단계는 WinFsp에 연결하기 전에 SFTP 비밀번호 인증, 호스트 키 검증,
원격 디렉터리 목록과 파일 읽기를 검증한다. 비밀번호는 파일이나 명령줄에
저장하지 않고 실행 시 숨김 입력으로 받는다.

## 호스트 키 등록

서버의 호스트 키 지문을 관리자 또는 서버 콘솔 등 별도 경로로 먼저 확인한다.
확인한 키만 사용자 `known_hosts`에 등록한다.

```powershell
New-Item "$HOME\.ssh" -ItemType Directory -Force
ssh-keyscan -p 22 example.com | Out-File "$HOME\.ssh\known_hosts" -Append -Encoding ascii
```

`ssh-keyscan` 결과를 검증하지 않고 신뢰하면 중간자 공격을 탐지할 수 없다.

## 디렉터리 목록

```powershell
go build -o bin/dkdrive-sftp.exe ./cmd/dkdrive-sftp
.\bin\dkdrive-sftp.exe -host example.com -port 22 -user tester -root /data
```

명령 실행 후 `SFTP 비밀번호:` 프롬프트가 나타나며 입력 내용은 화면에
표시되지 않는다. 자동화가 필요한 경우에만 현재 프로세스의
`DKDRIVE_SFTP_PASSWORD` 환경변수를 사용할 수 있다.

## 파일 읽기

```powershell
.\bin\dkdrive-sftp.exe -host example.com -port 22 -user tester -root /data -read hello.txt
```

이 명령은 연결·목록·읽기만 검증한다. 쓰기와 WinFsp 마운트는 다음 단계에서
별도로 연결한다.

## 개인키 인증

`-key`를 지정하면 비밀번호 대신 OpenSSH 개인키 인증을 사용한다.

```powershell
.\bin\dkdrive-sftp.exe `
    -host example.com -port 22 -user tester -root /data `
    -key "$HOME\.ssh\id_ed25519"
```

암호화된 개인키이면 `개인키 Passphrase:` 프롬프트가 나타나며 입력 내용은
표시되지 않는다. 키 파일 내용과 Passphrase는 설정 파일이나 명령줄에
저장하지 않는다.

## 검증 결과

- 날짜: 2026-08-26
- 환경: Synology SFTP 서버, 비표준 포트
- 통과: `known_hosts` 기반 호스트 키 검증
- 통과: 비밀번호 인증
- 통과: 원격 루트 디렉터리 목록과 한글 디렉터리명
- 통과: 하위 경로의 텍스트 파일 읽기

실제 서버 주소, 포트, 사용자명, 비밀번호와 조회 파일 내용은 저장소에
기록하지 않는다.

## 쓰기 작업 검증

`-write-test`를 지정하면 원격 시작 경로 아래에 고유한 `DKDrive 쓰기 테스트`
폴더를 만들고 파일 생성·쓰기·읽기 검증·이동·이름 변경·수정 시간·삭제를
순서대로 수행한다. 이 옵션을 지정하지 않으면 원격 파일을 변경하지 않는다.

```powershell
.\bin\dkdrive-sftp.exe `
    -host example.com -port 22 -user tester -root /data `
    -write-test
```

실패 시 프로그램이 자신이 만든 임시 폴더를 정리하려고 시도한다. 정리에
실패한 경우 출력된 `DKDrive 쓰기 테스트 ...` 경로만 직접 확인한다.

## 쓰기 검증 결과

- 날짜: 2026-08-26
- 통과: 파일 생성·쓰기·재읽기·삭제
- 통과: 폴더 생성·파일 이동·이름 변경·수정 시간 설정
- 통과: 한글과 공백이 포함된 테스트 경로
- 정리 확인: 검증용 파일과 폴더 삭제

테스트 서버에서는 `/`를 원격 시작 경로로 지정했을 때 목록과 읽기는
가능했지만 쓰기 권한이 없었다. 쓰기가 허용된 사용자 경로를 원격 시작
경로로 지정한 뒤 검증에 통과했다. 서버별 권한과 공유 경로 구성이 다르므로
마운트할 경로에서 쓰기 권한을 별도로 확인해야 한다.
