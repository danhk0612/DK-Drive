#ifndef AppVersion
  #error AppVersion is required
#endif
#ifndef SourceDir
  #error SourceDir is required
#endif
#ifndef OutputDir
  #error OutputDir is required
#endif
#ifndef WinFspFile
  #error WinFspFile is required
#endif
#ifndef WinFspSHA256
  #error WinFspSHA256 is required
#endif

[Setup]
AppId=DK-Drive.Chambitbada
AppName=DK-Drive
AppVersion={#AppVersion}
AppVerName=DK-Drive {#AppVersion}
AppPublisher=참빛바다
AppPublisherURL=https://github.com/danhk0612/DK-Drive
AppSupportURL=https://github.com/danhk0612/DK-Drive/issues
AppUpdatesURL=https://github.com/danhk0612/DK-Drive/releases
VersionInfoVersion={#AppVersion}.0
VersionInfoCompany=참빛바다
VersionInfoDescription=DK-Drive installer
VersionInfoProductName=DK-Drive
VersionInfoProductVersion={#AppVersion}
DefaultDirName={autopf}\DK-Drive
DefaultGroupName=DK-Drive
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\dkdrive.exe
LicenseFile={#SourceDir}\LICENSE
OutputDir={#OutputDir}
OutputBaseFilename=DK-Drive-{#AppVersion}-windows-amd64-setup
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
PrivilegesRequired=admin
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
CloseApplicationsFilter=dkdrive.exe
RestartApplications=no
SetupLogging=yes

[Tasks]
Name: "desktopicon"; Description: "바탕 화면에 바로가기 만들기"; GroupDescription: "추가 작업:"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Excludes: "{#WinFspFile}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\{#WinFspFile}"; Flags: dontcopy

[Icons]
Name: "{group}\DK-Drive"; Filename: "{app}\dkdrive.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\DK-Drive"; Filename: "{app}\dkdrive.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\dkdrive.exe"; Description: "DK-Drive 실행"; WorkingDir: "{app}"; Flags: nowait postinstall skipifsilent

[Code]
function WinFspInstalled: Boolean;
begin
  Result :=
    FileExists(ExpandConstant('{commonpf32}\WinFsp\bin\winfsp-x64.dll')) and
    RegKeyExists(HKLM, 'SYSTEM\CurrentControlSet\Services\WinFsp');
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  InstallerPath: String;
  ActualHash: String;
  ResultCode: Integer;
begin
  Result := '';
  if WinFspInstalled then
    Exit;

  ExtractTemporaryFile('{#WinFspFile}');
  InstallerPath := ExpandConstant('{tmp}\{#WinFspFile}');
  ActualHash := GetSHA256OfFile(InstallerPath);
  if CompareText(ActualHash, '{#WinFspSHA256}') <> 0 then
  begin
    Result := '내장된 WinFsp 설치 파일의 SHA-256 검증에 실패했습니다.';
    Exit;
  end;

  WizardForm.StatusLabel.Caption := '필수 구성 요소 WinFsp를 설치하는 중입니다...';
  if not Exec(ExpandConstant('{sys}\msiexec.exe'),
    '/i "' + InstallerPath + '" /qn /norestart', '', SW_HIDE,
    ewWaitUntilTerminated, ResultCode) then
  begin
    Result := 'WinFsp 설치 프로그램을 실행하지 못했습니다.';
    Exit;
  end;

  case ResultCode of
    0:
      Result := '';
    3010:
      begin
        NeedsRestart := True;
        Result := '';
      end;
    1602:
      Result := 'WinFsp 설치가 취소되어 DK-Drive 설치를 계속할 수 없습니다.';
  else
    Result := Format('WinFsp 설치에 실패했습니다. 오류 코드: %d', [ResultCode]);
  end;
end;
