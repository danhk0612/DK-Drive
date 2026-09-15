# 탐색 메타데이터 성능 기준 측정

## 현재 단계

Phase 2 계측만 구현했다. 메타데이터 캐시·요청 병합·HTTP Transport 설정은 아직
변경하지 않았다. Windows 결과가 중복 요청 가설과 일치하는지 확인한 뒤 Phase 3으로 진행한다.

`DKDRIVE_DIAGNOSTICS`에 출력 디렉터리를 지정한 GUI 실행에서만 계측한다.
연결마다 임의 이름의 JSON 하나를 만들고 backend가 닫힐 때 최종 집계를 기록한다.
환경변수가 없으면 기존 backend를 그대로 사용하며 진단 파일을 만들지 않는다.
GUI 설정·저장 프로필·자격 증명 형식은 바꾸지 않는다. 기존 프로토콜별 CLI는
이 환경변수를 읽지 않으며 이번 측정은 GUI 마운트 대상이다.

기록 내용은 스키마·프로토콜·시작 시각·세션 경과 시간과 아래 8종 연산의
호출 수·실패 횟수·총 소요 시간·최대 소요 시간뿐이다. 서버 주소·프로필 이름·
드라이브 문자·원격 경로·계정·비밀번호·오류 원문·HTTP 본문은 기록하지 않는다.
진단 기록 실패가 이미 완료된 연결 해제를 실패로 바꾸지는 않는다.

| 지표 | 계측 경계 |
| --- | --- |
| logical.stat / logical.readdir | 공통 VFS backend 호출. 잠금·재연결·응답 처리를 포함 |
| webdav.propfind.depth0 / depth1 | PROPFIND 호출별 응답 본문 해석까지. 초기 루트 확인 포함 |
| ftp.getentry | 원격 GetEntry 시도. 재접속 후 재시도도 별도 집계 |
| ftp.list | List 시도. Stat의 부모 목록 fallback과 재시도 포함 |
| sftp.stat | 실제 client.Stat 시도. 연결·재연결 루트 확인과 속성 설정용 조회 포함 |
| sftp.readdir | 실제 client.ReadDir 시도. 재시도 포함 |

이 수치는 원격 클라이언트 연산의 시도 횟수다. FTP List의 데이터 연결 명령이나
SFTP ReadDir의 여러 프로토콜 패킷, HTTP 내부 redirect/transport 재시도를 개별
패킷·요청으로 세지는 않는다. FTP 루트 Stat은 네트워크 조회 없이 반환될 수 있다.
논리·프로토콜 시간은 서로 포함 관계이므로 합산하지 않는다. 총 시간은 동시 호출로
세션 경과 시간보다 커질 수 있다. 타이머 해상도보다 짧은 호출은 0ns로 집계될 수 있다. dirty 쓰기 반영은 VFS Stat 이전에 일어나므로
별도 전송 시간은 논리 Stat 지표에 포함되지 않는다.

## Windows 실행 절차

1. 기존 DK-Drive를 정상 종료한다. 이미 실행 중이면 새 실행이 기존 창만 복원하므로
   진단 환경변수가 적용되지 않는다.
2. 테스트할 프로필 하나만 연결한다. 자동 연결된 다른 프로필이 있으면 해제한다.
   측정 중 연결 테스트·복구 재시도·다른 파일 작업은 하지 않는다.
3. 동일 서버의 기존 읽기 가능한 폴더를 사용한다. 먼저 약 100개 폴더로 WebDAV·FTPS·
   SFTP를 비교하고, 가능하면 약 20개 및 500개 이상 폴더도 같은 순서로 측정한다.
   파일을 생성하거나 삭제할 필요는 없다.
4. 탐색기는 자세히 보기, 미리보기 창 끔, 썸네일 대신 아이콘 표시로 조건을 맞춘다.
   평소 설정을 유지했다면 그 상태를 결과에 적는다.

저장소 폴더의 PowerShell에서 계측 브랜치를 가져오고 빌드한다.
기존 변경 파일이 있으면 덮어쓰지 말고 별도 checkout에서 실행한다.

```powershell
git fetch origin work/metadata-diagnostics
git switch --detach FETCH_HEAD
git rev-parse HEAD
go test ./...
if ($LASTEXITCODE -ne 0) { throw '테스트 실패' }
go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
if ($LASTEXITCODE -ne 0) { throw '빌드 실패' }
```

아래 블록을 프로토콜·폴더 규모 조합별로 한 번씩 실행한다. 실행 파일이 떠 있는
동안 다음 절차대로 탐색하고 **트레이에서 프로그램 종료**하면 집계표가 출력된다.
`$RunLabel`에는 서버나 개인 파일명 대신 `webdav-100-baseline` 같은 구분만 쓴다.

```powershell
$RunLabel = 'webdav-100-baseline'
$RunStamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$DiagnosticDir = Join-Path $env:TEMP "DKDrive-Metadata-$RunStamp"
$PreviousDiagnosticDir = $env:DKDRIVE_DIAGNOSTICS
try {
    $env:DKDRIVE_DIAGNOSTICS = $DiagnosticDir
    Start-Process -FilePath (Resolve-Path '.\bin\dkdrive.exe').Path -Wait
}
finally {
    $env:DKDRIVE_DIAGNOSTICS = $PreviousDiagnosticDir
}
$Summary = foreach ($ReportFile in Get-ChildItem $DiagnosticDir -Filter '*.json') {
    if ($ReportFile.Length -eq 0) {
        Write-Warning "미완성 진단 파일: $($ReportFile.Name)"
        continue
    }
    $Report = Get-Content $ReportFile.FullName -Raw | ConvertFrom-Json
    foreach ($Metric in $Report.metrics) {
        [pscustomobject]@{
            Run = $RunLabel
            Report = $ReportFile.Name
            Protocol = $Report.protocol
            Operation = $Metric.operation
            Calls = $Metric.calls
            Errors = $Metric.errors
            TotalMs = [math]::Round($Metric.total_ns / 1000000, 2)
            MaxMs = [math]::Round($Metric.max_ns / 1000000, 2)
        }
    }
}
$Summary | Format-Table -AutoSize
$Summary | Export-Csv (Join-Path $DiagnosticDir 'summary.csv') -NoTypeInformation -Encoding UTF8
$DiagnosticDir
```

탐색 순서와 대기 시간을 각 실행에서 같게 유지한다.

1. 연결 완료 후 대상 폴더로 이동: 목록이 모두 표시되기까지의 대략적인 시간을 기록.
2. 바로 하위 폴더로 이동: 표시 시간 기록.
3. 즉시 뒤로 이동: 재방문 표시 시간 기록.
4. 3초 이상 기다린 뒤 같은 하위 폴더 이동·뒤로 이동을 한 번 더 수행.
5. 탐색기 창을 닫고 해당 드라이브를 일반 해제한 뒤 프로그램 종료.

수동 표시 시간은 사람이 관찰한 근삿값이며 JSON의 연산 시간과 별도다.
세션 집계에는 연결 시 루트 확인·탐색기 배경 조회·종료 전 조회도 포함된다.
연결 시도나 프로필이 여러 개면 JSON도 여러 개다. 파일별 결과를 합치지 않는다.
비정상 강제 종료·Windows 종료 제한시간 초과 시 파일이 비어 있을 수 있으므로
그 실행은 기준 측정으로 사용하지 않는다. 일반 해제에 실패하면 상태를 기록하고
기존 UI 절차를 사용한다. 이번 읽기 측정에서 쓰기 장애를 인위적으로 만들지 않는다.

## 제출할 결과

- 빌드 커밋과 `summary.csv` 또는 JSON 파일.
- 프로토콜, 대략적인 항목 수, 썸네일·미리보기 여부.
- 최초 표시 / 하위 이동 / 즉시 뒤로 이동 / 3초 후 재방문의 대략적인 초 수.
- 일반 해제 성공 여부, 표시 오류나 멈춤 여부.
- 가능하면 같은 폴더의 RaiDrive 관찰값. 필수 합격 기준은 아니다.

계측 모드를 끄려면 위 실행 블록 밖에서 평소처럼 프로그램을 실행한다.
블록은 환경변수를 이전 값으로 복원하며 사용자 또는 시스템 환경변수를 저장하지 않는다.

## 자동 검증 결과와 판정

- counting backend: `ReadDir(/photo) → 자식 Stat 3회 → ReadDir(/photo)`에서
  backend Stat 3회·ReadDir 2회. 읽기 전용 wrapper와 종료 전달도 확인했다.
- FTP 모의 서버: MLST·MLSD 실패 후 재시도가 각각 2회, 실패 1회로 서버 카운트와 일치.
- WebDAV 모의 서버: Depth별 횟수와 권한 오류 집계 확인.
- SFTP 로컬 프로토콜 서버: Stat·ReadDir 집계와 호출 전 취소의 원격 요청 미발생 확인.
- 동시 집계 race, 연결별 분리, 비밀 문자열 제외, 비활성 모드와 출력 검증.

동일 폴더 반복 탐색의 원격 조회 수와 지연이 반복 메타데이터 가설에 부합하면
Phase 3–6 캐시·자식 메타데이터 재사용·무효화·동시 요청 병합을 구현한다.
이 단계의 테스트 결과는 캐시 효과나 실제 탐색기 속도 개선의 증거가 아니다.
