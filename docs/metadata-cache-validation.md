# 메타데이터 캐시 결과와 Windows 재검증

## 수신한 기준 결과 (2026-09-15)

빌드: `2f01ab4ecd3d92bde76f7dd6a3794700c8bc9bce`. 사용자 Windows test/vet/build 통과.

| 프로토콜 | 논리 Stat (실패) | 논리 ReadDir | 원격 메타데이터 (실패) | 원격 목록 |
| --- | ---: | ---: | ---: | ---: |
| WebDAV | 5,642 (76) | 41 | Depth 0: 5,643 (76) | Depth 1: 41 |
| Explicit FTPS | 6,114 (107) | 50 | GetEntry: 5,614 (107) | List: 157 |

WebDAV 파일은 `dkdrive-metadata-159903581.json`, FTPS 파일은
`dkdrive-metadata-650032476.json`이다. FTPS ZIP에 함께 든 WebDAV
`dkdrive-metadata-299212473.json`은 루트 확인 1회·논리 호출 0회이므로 탐색 분석에서 제외한다.
폴더명 라벨 대신 JSON의 protocol로 판정한다. 중복 실행이 소스나 기존 결과를 망가뜨린 증거는 없다.
SFTP 실행 기록은 있지만 결과 JSON은 제출되지 않았다.

WebDAV 논리 Stat 누적 시간은 43.3561788초, Depth 0은 43.3657822초다.
FTPS 논리 Stat은 8.5664512초, GetEntry는 6.0303027초다.
이 시간들은 중첩되므로 합산하지 않는다. FTPS List 157회는 논리 ReadDir 50회와
실패한 GetEntry 107회의 fallback 수에 부합한다. 실패의 구체 원인은 이 집계로 확정하지 않는다.
경로·항목 수·수동 표시 시간이 없어 정확한 중복 비율이나 프로토콜 간 속도 우열은 계산하지 않는다.
Stat 위주의 원격 조회를 줄이는 구현 근거로 사용하며, 실제 개선율은 후속 측정 대상이다.

## 구현 범위

- GUI 마운트 연결마다 독립 캐시. Stat/ReadDir 2초, 단일 오류 체인으로 확인된 NotFound 500ms.
- ReadDir 결과의 직접 자식 정보를 Stat에 제공. 대소문자 유지, 반환 slice 복사, traversal은 원래 backend가 판정.
- 최대 4,096개 키·추정 메타데이터 16MiB. 한도 압박 시 저장 항목을 비운다. 단일 초과 결과는 저장하지 않는다.
  이는 보유 캐시의 계산상 한도이며 네트워크 응답·동시 요청·Go 객체 전체 메모리의 엄격한 상한은 아니다.
- 생성·삭제·이동·속성 변경·OpenWrite/WriteAt/Sync/Close 전후 관련 경로와 부모 목록 무효화.
  폴더 이동은 양쪽 하위 경로도 제거한다. 실패했어도 부분 반영 가능성을 고려해 무효화한다.
- 변이 중에는 캐시와 병합을 우회하고, 이전 세대의 응답은 캐시를 다시 채우지 못한다.
- 동일 경로·연산 요청 병합. 대기자의 취소는 다른 요청을 취소하지 않는다.
  첫 호출이 취소되면 살아 있는 대기자가 다시 조회한다. 다른 경로·연산은 병렬 실행한다.
- 복구용 OpenBackend는 캐시를 사용하지 않으므로 복구 충돌 검사는 항상 원격을 조회한다.
- 파일 내용·복구 staging은 별도다. dirty staging 반영 후 Stat 순서를 유지한다.
- 논리 계측은 캐시 바깥, 프로토콜 계측은 안쪽에 두어 원격 호출 감소를 비교한다.
  진단 환경변수 없이도 마운트 캐시는 동작한다. HTTP Transport와 설정 형식은 변경하지 않았다.

자동 탐색 패턴의 원격 Stat/ReadDir는 기존 3/2회에서 0/1회로 감소했다.
WebDAV 모의 서버에서도 자식 Stat의 추가 PROPFIND 없이 같은 결과를 반환한다.
TTL·오류 종류·쓰기 실패·이동·읽기 전용·동시 호출·취소·오래된 응답을 검사했다.
Windows 전용 테스트는 dirty 쓰기 후 크기 조회와 실패 업로드 강제 해제의 로컬 데이터 보존을 검사한다.
이 검사는 실제 NAS/WinFsp 탐색 속도 검증을 대신하지 않는다.

## Windows 빌드

기존 프로그램을 트레이에서 종료한 뒤 저장소 PowerShell에 블록 전체를 붙여 넣는다.

```powershell
& {
    $ErrorActionPreference = 'Stop'
    Set-Location 'D:\DK-Drive\DK-Drive'
    if (Get-Process dkdrive -ErrorAction SilentlyContinue) { throw '트레이에서 DK-Drive를 종료하세요.' }
    $Changes = git status --porcelain
    if ($LASTEXITCODE -ne 0) { throw 'Git 확인 실패' }
    if ($Changes) { $Changes; throw '로컬 변경이 있습니다. 출력을 보내주세요.' }
    git fetch origin work/metadata-cache
    if ($LASTEXITCODE -ne 0) { throw '소스 가져오기 실패' }
    git switch --detach FETCH_HEAD
    if ($LASTEXITCODE -ne 0) { throw '소스 전환 실패' }
    git log -1 --oneline
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw '테스트 실패' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw '정적 검사 실패' }
    go build -ldflags="-H=windowsgui" -o bin/dkdrive.exe ./cmd/dkdrive
    if ($LASTEXITCODE -ne 0) { throw '빌드 실패' }
}
```

## 한 번에 수행할 재검증

먼저 WebDAV, 다음 Explicit FTPS에서 아래 실행 블록을 한 번씩 사용한다.
RunLabel은 결과 폴더 구분이며 실제 프로토콜은 GUI에서 선택한다.
사용하지 않는 서버를 새로 준비할 필요는 없다. SFTP는 사용 가능할 때만 수행한다.

```powershell
& {
    $ErrorActionPreference = 'Stop'
    if (Get-Process dkdrive -ErrorAction SilentlyContinue) { throw '기존 DK-Drive를 종료하세요.' }
    $RunLabel = 'webdav-cache' # 다음 실행은 ftps-cache
    $DiagnosticDir = Join-Path $env:TEMP ("DKDrive-$RunLabel-" + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
    $PreviousDiagnosticDir = $env:DKDRIVE_DIAGNOSTICS
    try {
        $env:DKDRIVE_DIAGNOSTICS = $DiagnosticDir
        Start-Process -FilePath (Resolve-Path '.\bin\dkdrive.exe').Path -Wait
    }
    finally { $env:DKDRIVE_DIAGNOSTICS = $PreviousDiagnosticDir }
    $Reports = @(Get-ChildItem -LiteralPath $DiagnosticDir -Filter '*.json')
    if ($Reports.Count -eq 0) { throw '진단 JSON이 없습니다.' }
    $ResultZip = "$DiagnosticDir.zip"
    Compress-Archive -LiteralPath $Reports.FullName -DestinationPath $ResultZip
    Write-Host "제출할 파일: $ResultZip"
    explorer.exe "/select,`"$ResultZip`""
}
```

프로그램이 떠 있는 동안:

1. 해당 프로필만 연결하고 이전 측정과 같은 폴더·보기 설정을 사용한다.
2. 최초 진입 → 하위 폴더 → 즉시 뒤로 → 3초 대기 → 하위 폴더 → 뒤로 순서로 탐색한다.
   각 표시 시간과 대략적인 파일 수, 썸네일·미리보기 여부를 적는다.
3. 탐색기 창을 닫고 일반 해제를 확인한다. 트레이에서 종료하면 ZIP이 만들어진다.
4. **성능 측정 종료 후** 평소 방식으로 다시 실행해 별도 임시 테스트 폴더에서
   새 텍스트 저장·즉시 재열기·더 긴 내용으로 덮어쓰기·즉시 크기와 내용 확인,
   파일 이름 변경·폴더 이름 변경·이동·삭제를 수행한다. 기존 사용자 파일은 사용하지 않는다.
5. 읽기 전용 프로필에서는 테스트 파일 읽기를 확인하고 덮어쓰기·이름 변경·삭제가
   거부되는지 확인한다. 다른 클라이언트에서 테스트 파일을 변경했다면 3초 후 새로고침해 반영을 확인한다.
6. 일반 해제·정상 종료 결과와 오류/멈춤 여부를 기록한다. 업로드 실패 보존과 로그인·시스템 종료,
   장시간 단절은 기존 잔여 검증 항목이며 이번 탐색 측정에 섞지 않는다.

ZIP 두 개와 표시 시간·항목 수·기능 검증 결과를 제출한다. 이전 기준을 다시 실행할 필요는 없다.
기준 당시의 순서·조건이 달랐다면 전후 속도 비율은 확정하지 않고 호출 감소와 관찰 결과를 따로 기록한다.

## 캐시 적용 후 수신 결과 (2026-09-15)

사용자가 제공한 후속 실행 결과이며 실행 커밋 출력은 별도로 제출되지 않았다.
직전 안내 빌드는 `a52c737`이다. JSON은 빌드 커밋을 포함하지 않는다.

| 프로토콜 | 논리 Stat 전 → 후 | 원격 Stat 계열 전 → 후 | 논리 ReadDir 전 → 후 | 원격 목록 전 → 후 |
| --- | ---: | ---: | ---: | ---: |
| WebDAV | 5,642 → 8,180 | Depth 0: 5,643 → 42 | 41 → 54 | Depth 1: 41 → 31 |
| Explicit FTPS | 6,114 → 6,062 | GetEntry: 5,614 → 31 | 50 → 47 | List: 157 → 53 |

후속 파일: WebDAV `dkdrive-metadata-2559217590.json`, Explicit FTPS
`dkdrive-metadata-2534687151.json`. 두 ZIP 모두 webdav 라벨이지만 JSON protocol로 분류했다.
함께 들어온 `dkdrive-metadata-455418605.json`은 짧은 WebDAV 세션이며
논리 Stat 16회·ReadDir 0회라서 대표 탐색 비교에서 제외했다.

원격 Stat 계열 집계는 각각 약 99.26%, 99.45% 감소했다. 실행 길이와 탐색 조건이
완전히 통제되지 않았으므로 이는 수신된 집계 사이의 차이이며 캐시 적중률이나
정확한 속도 개선율이 아니다. 사용자는 “훨씬 빨라졌음”을 보고했다.
논리 호출 대비 원격 호출 감소와 체감 개선을 함께 확인했다.
후속 원격 오류는 WebDAV Depth 0 19회, FTPS GetEntry 25회이며 목록 오류는 모두 0회다.
오류 원문이 없어 개별 원인은 확정하지 않는다.

읽기 전용에서 권한 요구로 쓰기 차단, 해제 후 정상 파일 쓰기를 확인했다.
프로토콜별 쓰기 검증 구분과 기존 파일 덮어쓰기·이동·삭제 차단은 보고되지 않았다.
SFTP·폴더 규모별 표시 시간·일반 해제·쓰기 직후 크기와 내용은 미확인으로 유지한다.
HTTP Transport 추가 변경을 뒷받침하는 잔여 병목 근거는 없어 현재 단계에서 변경하지 않는다.

### 재검증 범위 정정

이미 통과한 기본 파일 작업·읽기 전용·일반 연결 해제 결과는 유지한다.
캐시 변경을 이유로 전체 기능을 다시 실행하도록 요구하지 않는다.
추가로 확인할 범위는 저장·덮어쓰기 직후 내용과 크기, 이름 변경·이동·삭제 후
이전 항목이 남는지뿐이다. 새 빌드에서 이미 확인했다면 재실행하지 않는다.
현재 새 빌드의 해당 개별 동작에 대한 명시적 보고는 없으므로 자동 테스트 통과와
이전 실환경 검증을 구분해 기록한다. 로그인·시스템 종료·실제 업로드 실패 복구는
캐시 회귀와 별개로 기존 미확인 항목이다.


---
WinFsp - Windows File System Proxy, Copyright (C) Bill Zissimopoulos

[WinFsp 저장소](https://github.com/winfsp/winfsp) · GPLv3 + FLOSS 예외 적용. DK-Drive 자체는 MIT.
