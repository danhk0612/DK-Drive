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
