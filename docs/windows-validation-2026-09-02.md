# 0.6 일괄 Windows 검증 — 2026-09-02

이 문서는 원격 재시도, 캐시 사용량, 연결 진단과 최근 트레이·해제 회귀를 한 번에
확인하기 위한 순서다. 기존 운영 파일은 변경하지 않고 `DK-Drive 검증` 아래에서 만든
격리 파일만 사용한다. 비밀번호, Passphrase, 개인키 내용과 서버 주소는 기록하지 않는다.

## 1. 업데이트와 자동 검증

실행 중인 DK-Drive와 기존 CLI 마운트를 먼저 정상 종료한다.

```powershell
Set-Location 'D:\DK-Drive\DK-Drive'
git pull
git rev-parse --short HEAD
go test ./...
go vet ./...
go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
```

실패하면 GUI 검증을 시작하지 않고 전체 오류 출력을 보관한다. 성공하면 실행한다.

```powershell
.\bin\dkdrive.exe
```

## 2. 최근 UI·트레이·일반 해제 회귀

- 프로그램명·창 제목·트레이 Tooltip이 `DK-Drive`인지 확인
- 트레이 최상위 실행 항목이 `DK-Drive 실행`인지 확인
- 트레이 `드라이브` 하위에 각 프로필, `드라이브 추가`, `모두 연결`, `모두 해제`가
  들어 있고 최상위에 중복 항목이 없는지 확인
- `캐시 관리` 하위에 `보존 캐시 열기`, `캐시 폴더 열기`, `캐시 정리`가 있는지 확인
- 쓰기 가능 대표 프로필을 연결한 직후 파일을 열지 않고 바로 일반 해제한다. 강제
  해제 질문 없이 드라이브가 사라져야 한다.
- 다시 연결하고 탐색기에서 루트를 연 상태로 일반 해제한다. 단순 디렉터리 열거 핸들은
  자동 정리되어 강제 해제 질문 없이 해제되어야 한다.
- 연결 이름과 탐색기 볼륨명이 같은지 확인한다.

```powershell
Get-Volume | Where-Object DriveLetter | Select-Object DriveLetter, FileSystemLabel
```

## 3. 연결 오류와 진단 표시

운영 프로필을 수정하지 말고 별도 테스트 프로필을 만든다.

1. 이름 `진단 테스트`, 사용하지 않는 드라이브 문자, 기존과 같은 프로토콜을 선택한다.
2. 호스트는 연결될 수 없는 테스트용 `.invalid` 이름으로 저장하고 연결한다.
3. 목록 상태가 `오류`가 되고 드라이브가 생기지 않는지 확인한다.
4. 프로필을 다시 선택해 상태 영역에 `상태: 오류`, 마지막 오류, 발생 시각,
   `보존 캐시: n개 / 용량`이 표시되는지 확인한다.
5. 오류 상태에서도 연결·삭제 버튼이 활성화되고 같은 프로필로 즉시 재시도할 수
   있는지 확인한다. `Test-Path '선택문자:\'`는 `False`여야 한다.
6. 테스트 프로필을 삭제한다.
7. 실제 프로필에서 일시적 잘못된 인증으로 오류를 만든 뒤 올바른 인증으로 재연결한다.
   성공 후 이전 오류 문구가 사라지고 상태가 `연결됨`인지 확인한다.

## 4. 원격 파일이 없는 보존 항목 만들기

쓰기 가능 대표 프로필을 연결하고 테스트 폴더를 만든다. 아래 예시의 `Y:`는 실제
테스트 드라이브 문자로 바꾼다.

```powershell
$Drive = 'Y:'
$Run = Get-Date -Format 'yyyyMMdd-HHmmss'
$RemoteDir = "$Drive\DK-Drive 검증\recovery-$Run"
New-Item -ItemType Directory -Path $RemoteDir -Force | Out-Null
$RemoteFile = Join-Path $RemoteDir 'force-preserved.txt'
$Bytes = [System.Text.UTF8Encoding]::new($false).GetBytes('DK-Drive 원격 재시도 검증')
$RecoveryStream = [System.IO.File]::Open(
    $RemoteFile,
    [System.IO.FileMode]::CreateNew,
    [System.IO.FileAccess]::Write,
    [System.IO.FileShare]::ReadWrite
)
$RecoveryStream.Write($Bytes, 0, $Bytes.Length)
```

`Flush()`와 `Dispose()`는 아직 호출하지 않는다. GUI에서 해당 드라이브를 일반 해제한
뒤 강제 해제 질문에서 `예`를 선택한다. 드라이브 제거와 캐시 위치 안내를 확인한 후
스트림을 정리한다.

```powershell
try { $RecoveryStream.Dispose() } catch { $_.Exception.Message }
$RecoveryStream = $null
```

같은 프로필을 다시 연결한 뒤 `Test-Path $RemoteFile`이 `False`인지 확인하고 다시 정상
해제한다. 복구 창을 열어 새 항목의 프로필·프로토콜·원격 경로·
크기·보존 시각·사유가 표시되고 프로그램 재시작만으로 자동 업로드되지 않는지 확인한다.

## 5. 캐시 사용량 표시

- 메인 버튼이 `보존 캐시 (항목 수 · 용량)` 형태인지 확인
- 복구 창 상단의 전체 캐시, 복구 목록 수·바이트, 메타데이터 없는 항목 수·바이트 확인
- 4번 항목을 선택해 상세 영역의 프로필별 보존 항목 수·바이트 확인
- `%LOCALAPPDATA%\DKDrive\Cache`의 실제 파일 크기 합계와 표시가 합리적으로 맞는지
  확인한다. 전체 캐시는 JSON 메타데이터 크기를 포함하므로 복구 데이터보다 클 수 있다.

```powershell
$CachePath = Join-Path $env:LOCALAPPDATA 'DKDrive\Cache'
Get-ChildItem $CachePath -Force | Select-Object Name, Length, LastWriteTime
($Files = Get-ChildItem $CachePath -File -Force | Measure-Object Length -Sum).Sum
```

## 6. 원격 파일 없음 재시도와 캐시 유지

1. 메인 창에서 4번 항목과 같은 쓰기 가능 프로필을 선택하되 마운트는 해제 상태로 둔다.
2. 복구 창에서 항목을 선택하고 `원격 재시도`를 누른다.
3. 원격 파일 없음 → 전송 → 원격 크기 확인 완료 안내를 확인한다.
4. 캐시 삭제 질문에서는 `아니요`를 선택한다.
5. 프로필을 연결해 원격 내용이 `DK-Drive 원격 재시도 검증`인지 확인한다.
6. 복구 항목과 메타데이터가 그대로 남는지 확인한다.

## 7. 충돌의 건너뛰기·다른 이름·덮어쓰기

프로필을 연결해 원격 원본을 다른 내용으로 바꾼 뒤 정상 해제한다.

```powershell
[System.IO.File]::WriteAllText(
    $RemoteFile,
    '원격에서 변경한 충돌 내용',
    [System.Text.UTF8Encoding]::new($false)
)
```

각 재시도는 마운트를 해제한 상태에서 수행한다.

1. 충돌 안내에서 `취소`를 선택한다. 원격 변경 내용과 캐시가 모두 유지되어야 한다.
2. 다시 재시도해 `아니요`를 선택한다. `.recovered-날짜-시각`이 붙은 다른 이름으로
   전송되고 원본 원격 파일은 변경되지 않아야 한다. 캐시 삭제는 `아니요`를 선택한다.
3. 다시 재시도해 `예`를 선택한다. 기존 원격 파일이 보존 캐시 내용으로 덮어써져야
   한다. 캐시 삭제는 다시 `아니요`를 선택한다.
4. 서버가 수정 시각을 제공하지 않으면 크기가 같아도 충돌로 처리되는 것이 정상이다.
   수정 시각을 제공하고 로컬과 2초 이내로 같을 때만 업로드 생략 안내가 나와야 한다.

## 8. 재시도 중 창 닫기와 연결 중 캐시 삭제 차단

전송 시간이 눈에 보이는 큰 보존 항목이 있을 때 재시도 직후 복구 창의 X와 `닫기`를
각각 누른다. `원격 재시도가 끝날 때까지 ... 닫을 수 없습니다` 안내가 나오고 창과
프로그램이 유지되어야 한다. 큰 항목이 없거나 전송이 즉시 끝나면 이 항목은 결과에
`재현 불가(전송이 너무 빠름)`로 기록한다.

필요하면 4번과 같은 방식으로 `force-preserved-large.bin`을 열고 아래 128 MiB를 쓴 뒤
`Flush()`·`Dispose()` 전에 강제 해제하여 큰 보존 항목을 만든다.

```powershell
$LargeFile = Join-Path $RemoteDir 'force-preserved-large.bin'
$LargeStream = [System.IO.File]::Open(
    $LargeFile,
    [System.IO.FileMode]::CreateNew,
    [System.IO.FileAccess]::Write,
    [System.IO.FileShare]::ReadWrite
)
$LargeBlock = [byte[]]::new(1MB)
1..128 | ForEach-Object { $LargeStream.Write($LargeBlock, 0, $LargeBlock.Length) }
```

재시도 성공 후 캐시를 남긴 상태에서 프로필을 연결하고 `선택 항목 삭제`와 트레이
`캐시 정리`를 각각 시도한다. 둘 다 연결 중이라는 이유로 차단되고 원격/캐시 파일이
유지되어야 한다.

## 9. 최종 캐시 삭제 범위

모든 드라이브를 해제한다.

1. 선택 삭제 확인에서 `아니요`를 눌러 해당 항목과 다른 항목이 유지되는지 확인한다.
2. 다시 선택 삭제를 승인한다. 선택한 스테이징 파일과 연결 메타데이터만 사라지고
   캐시 루트와 다른 항목은 유지되어야 한다.
3. 필요한 복구 항목을 모두 내보낸 뒤에만 트레이 `캐시 정리`를 실행한다.
4. 첫 확인은 취소해 유지, 두 번째 확인은 승인해 캐시 내부만 비워지는지 확인한다.

```powershell
Test-Path $CachePath
Get-ChildItem $CachePath -Force
```

캐시 폴더는 `True`, 내부 목록은 비어 있어야 한다.

## 10. 실제 로그인 자동 실행·자동 연결

테스트가 끝난 뒤 저장한 자격 증명이 있는 한 프로필만 자동 연결을 켠다.
`Windows 로그인 시 실행`과 `창 닫으면 트레이로`를 켜고 로그아웃·로그인한다.

- 로그인 후 DK-Drive가 트레이에 한 번만 실행되는지 확인
- 지정 프로필만 자동 연결되고 다른 프로필은 연결되지 않는지 확인
- 작업 관리자와 트레이에 중복 프로세스/아이콘이 없는지 확인
- `DK-Drive 실행`으로 창이 복원되는지 확인
- 자동 연결 직후 아무 파일도 열지 않은 상태에서 일반 해제가 성공하는지 다시 확인

검증 후 자동 연결과 Windows 로그인 실행 설정은 원하는 운영 상태로 되돌린다.

## 결과 보고 형식

아래 항목만 순서대로 `통과 / 실패 / 재현 불가`로 기록하고, 실패한 항목에는 화면 문구와
PowerShell 출력만 붙인다. 자격 증명과 서버 주소는 제외한다.

1. 빌드·자동 테스트
2. UI·트레이·일반 해제
3. 연결 오류 진단
4. 보존 항목 생성
5. 캐시 사용량
6. 원격 파일 없음 재시도
7. 충돌 3가지 선택
8. 재시도 중 닫기·삭제 차단
9. 선택 삭제·전체 정리
10. 로그인 자동 실행·자동 연결
