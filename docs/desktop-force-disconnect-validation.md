# GUI 강제 해제 검증

상태: 2026-08-31 실제 WinFsp/GUI 기본 시나리오 통과. 업로드 실패 실서버 시나리오는 미검증.
기존 일반 해제 차단은 읽기 핸들 유지 → 해제 거부 → 핸들 닫기 → 해제로 확인했다.
이번 변경은 거부 이후 사용자에게 강제 해제 선택을 제공한다.

## 2026-08-31 실환경 결과

- WebDAV 읽기 핸들 유지 상태에서 승인: 드라이브 제거, 기존 핸들 작업 취소,
  캐시 위치 안내, 재연결 후 원격 원문 유지.
- 기본 선택 아니요: 별도 오류창 없이 WebDAV 드라이브와 기존 핸들 유지.
- FTPS·WebDAV 전체 해제에서 FTPS는 취소, WebDAV는 승인: FTPS와 해당 핸들만
  유지되고 WebDAV만 제거. FTPS 핸들 닫은 뒤 정상 해제 성공.
- 프로그램 종료에서 아니요: 프로세스·WebDAV·기존 핸들 유지. 다시 예를 선택하면
  캐시 안내 후 드라이브·프로세스 제거.
- 강제 해제 후 재연결 및 기존 UTF-8 파일 읽기 성공.

위 결과는 읽기 전용 프로필과 기존 파일의 열린 읽기 핸들 기준이다. 쓰기 업로드
실패, 실행 중 원격 전송과 로컬 동기화 실패를 실제 NAS에서 만들지는 않았다.

## 범위와 주의

- GUI 선택 해제, 트레이 개별 해제, 전체 해제, 프로그램 종료에 적용한다.
- 정상 해제가 성공하면 질문하지 않는다. 실패한 연결마다 예/아니요를 묻는다.
- 기본 선택은 **아니요**. 취소하면 해당 연결을 유지한다.
- 강제 해제는 연결을 끊고 DKDrive의 열린 임시 파일을 보존한다. 원격 업로드를
  재시도하거나 임시 파일을 자동 복구하지 않는다. 이미 실행 중인 작업은 기다릴 수 있다.
- 앱/Windows 버퍼의 미전달 데이터와 이미 시작한 원격 쓰기는 보존 보장 대상이 아니다.
- 실제 NAS 서비스 중지·네트워크 전체 차단·PC 로그오프/재부팅은 하지 않는다.
- 운영 파일은 읽기만 한다. 실패 시 캐시와 진단 파일은 삭제하지 않는다.

## 재검증 절차

아래 절차는 회귀 확인이 필요할 때 사용한다.

### 1. 준비

이전 GUI에서 파일을 모두 닫고 정상 종료한다. 실행 중인 EXE 위에 빌드하지 않는다.

```powershell
Set-Location 'D:\DK-Drive\DK-Drive'
git pull
git rev-parse HEAD
go test ./...
go vet ./...
go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
.\bin\dkdrive.exe
```

기존 FTPS `X:`와 WebDAV `Y:` 읽기 전용 프로필을 연결한다. 아래 예시는 두
드라이브에 `탐색기 이름 변경.txt`가 있다는 기존 테스트 환경 기준이다.
다른 환경에서는 이미 존재하는 읽기 가능한 파일 경로로 바꾼다.

### 2. 취소하면 유지, 핸들을 닫으면 일반 해제

별도 PowerShell에서 실행하고 같은 창을 유지한다.

```powershell
$HeldStream = [System.IO.File]::Open(
    'Y:\탐색기 이름 변경.txt',
    [System.IO.FileMode]::Open,
    [System.IO.FileAccess]::Read,
    [System.IO.FileShare]::ReadWrite
)
```

GUI에서 Y: 선택 해제 → 실패 원인·프로필 이름·드라이브·데이터 손실 경고 확인 →
**아니요** 선택. 기본 포커스도 아니요여야 한다.

취소 후 별도의 빨간 오류창을 표시하지 않고, 주 창 상태 영역에 연결 유지 결과만
표시해야 한다.

```powershell
Test-Path 'Y:\' # True
$HeldStream.ReadByte() # 오류 없이 읽힘
$HeldStream.Dispose()
$HeldStream = $null
```

Y: 선택 해제를 다시 누른다. 추가 질문 없이 성공하고 `Test-Path 'Y:\'`는 False,
`Test-Path 'X:\'`는 True여야 한다.

### 3. 승인하면 열린 핸들이 있어도 해당 연결 해제

Y:를 다시 연결한 후 2번의 `$HeldStream = ...` 블록으로 다시 연다.
트레이의 Y: 항목으로 해제 → 이번에는 **예** 선택 → 캐시 위치 안내 확인.

```powershell
Test-Path 'Y:\' # False
Test-Path 'X:\' # True
try {
    $HeldStream.ReadByte()
    Write-Host '주의: 기존 핸들에서 읽기가 반환됨 — 출력 보관'
}
catch {
    Write-Host "기존 핸들 읽기 실패: $($_.Exception.Message)"
}
finally {
    $HeldStream.Dispose()
    $HeldStream = $null
}
```

Windows가 이미 버퍼링한 읽기 결과를 반환할 가능성이 있으므로 기존 핸들 읽기
출력만으로 성공을 판정하지 않는다. 드라이브 제거와 재연결 후 읽기를 함께 확인한다.

Y: 재연결 후:

```powershell
Get-Content 'Y:\탐색기 이름 변경.txt' -Encoding UTF8 -ErrorAction Stop
```

기존 내용 유지, GUI 상태 연결됨, 같은 드라이브 문자 재사용을 확인한다.

### 4. 전체 해제에서 서로 다른 선택

X:와 Y:가 모두 연결된 상태에서 두 읽기 핸들을 연다.

```powershell
$HeldX = [System.IO.File]::OpenRead('X:\탐색기 이름 변경.txt')
$HeldY = [System.IO.File]::OpenRead('Y:\탐색기 이름 변경.txt')
```

모두 해제 → **X: 질문에는 아니요, Y: 질문에는 예**.
질문 순서가 아니라 표시된 드라이브로 구분한다. 두 질문 사이 다른 연결/설정
작업이 실행되지 않아야 한다.

```powershell
Test-Path 'X:\' # True
Test-Path 'Y:\' # False
$HeldX.Dispose()
$HeldY.Dispose()
$HeldX = $null
$HeldY = $null
```

이후 모두 해제 → X:도 정상 해제. 이미 해제한 Y:에 다시 질문하지 않아야 한다.

### 5. 프로그램 종료 중 취소/승인

Y:만 다시 연결하고 2번의 읽기 핸들을 연다.

1. 프로그램 종료 → 강제 해제 **아니요**: GUI/프로세스/Y: 유지.
2. 다시 프로그램 종료 → 강제 해제 **예**: 캐시 안내 확인 후 Y:·GUI·트레이 제거.
3. 아래 코드로 잔여 핸들을 닫고 상태 확인.

```powershell
$HeldStream.Dispose()
$HeldStream = $null
Test-Path 'Y:\' # False
Get-Process 'dkdrive' -ErrorAction SilentlyContinue # 출력 없음
```

재실행 후 기존 프로필·자동 연결 설정이 유지되는지도 확인한다.

## 자동 테스트로 먼저 확인할 부분

- 정상 해제는 확인창 미요청, 실패할 때만 확인 요청.
- 취소한 연결 유지, 승인한 연결만 강제 해제, 강제 해제 실패 시 예약 유지.
- 전체 해제 중 일부 취소/실패는 오류로 반환하여 프로그램 종료를 막음.
- 강제 해제 시 미업로드 쓰기를 추가 업로드하지 않고 로컬 파일 내용 보존.
- 이전 업로드 실패 캐시 보존, 업로드 재시도 없음.
- 강제 해제 후 추가 Write/Sync/열기 거부, 중복 Close가 캐시를 삭제하지 않음.

이 자동 테스트는 실제 Windows 버퍼, 실행 중인 NAS 전송, 실제 WinFsp 해제와
확인창 조작을 대신하지 않는다. 위 1~5 결과와 오류 메시지를 모아서 보고한다.
GUI SFTP·쓰기 전체 시나리오와 실제 로그인 자동 실행 등 기존 미검증 항목도 남겨 둔다.
