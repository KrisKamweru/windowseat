; Windows setup wizard. Build the binaries with scripts/build.sh first, then:
;   iscc /DAppVersion=1.2.0 /DArch=amd64 installer\windows\windowseat.iss
; The wizard copies the files and hands the rest to `windowseat install`,
; so the tasks, PATH and upgrade steps stay the same as a manual install.

#ifndef AppVersion
  #define AppVersion "0.1.0-dev"
#endif
#ifndef Arch
  #define Arch "amd64"
#endif
#if Arch == "amd64"
  #define InnoArch "x64compatible"
#elif Arch == "arm64"
  #define InnoArch "arm64"
#else
  #error Arch must be amd64 or arm64
#endif

; Task names from internal/install/schtasks.go.
#define ServiceTask "Windowseat"
#define TrayTask "Windowseat Tray"

[Setup]
AppId={{C6D6307E-C225-4FB6-A7F9-FBE68BC8FDB1}
AppName=Windowseat
AppVersion={#AppVersion}
AppPublisher=Kris Kamweru
AppPublisherURL=https://github.com/KrisKamweru/windowseat
PrivilegesRequired=lowest
; windowseat install always uses this folder, so the user can't pick another.
DefaultDirName={localappdata}\Programs\Windowseat
DisableDirPage=yes
DisableProgramGroupPage=yes
ArchitecturesAllowed={#InnoArch}
ArchitecturesInstallIn64BitMode={#InnoArch}
LicenseFile=..\..\LICENSE
SetupIconFile=..\..\assets\icon\favicon.ico
UninstallDisplayIcon={app}\windowseat.exe
UninstallDisplayName=Windowseat
OutputDir=..\..\dist
OutputBaseFilename=windowseat-{#AppVersion}-windows-{#Arch}-setup
WizardStyle=modern
; windowseat install starts the service and tray itself.
RestartApplications=no

[Tasks]
Name: tray; Description: "Show the tray icon"
Name: path; Description: "Add Windowseat to PATH"

[Files]
Source: "..\..\dist\windows-{#Arch}\windowseat.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\dist\windows-{#Arch}\windowseat-tray.exe"; DestDir: "{app}"; Flags: ignoreversion

[UninstallRun]
; Removes the tasks, PATH entry and saved state. It moves its own running exe
; to %TEMP% and deletes the tray exe; the uninstaller skips files already gone.
Filename: "{app}\windowseat.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "WindowseatUninstall"

[Code]
procedure EndTask(const Name: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\schtasks.exe'), '/End /TN "' + Name + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

{ Stop running copies so an upgrade can replace the exes. }
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  EndTask('{#ServiceTask}');
  EndTask('{#TrayTask}');
  Result := '';
end;

function InstallParams: String;
begin
  Result := 'install';
  if not WizardIsTaskSelected('tray') then
    Result := Result + ' --no-tray';
  if WizardIsTaskSelected('path') then
    Result := Result + ' --add-to-path';
end;

{ [Run] ignores exit codes, so run install here and report a failure. }
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep <> ssPostInstall then
    Exit;
  if not Exec(ExpandConstant('{app}\windowseat.exe'), InstallParams, ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    SuppressibleMsgBox('Windowseat was copied but could not be set up (exit code ' + IntToStr(ResultCode) + ').' + #13#10#13#10 +
      'Run this in a terminal to see why:' + #13#10 + ExpandConstant('"{app}\windowseat.exe" ') + InstallParams,
      mbError, MB_OK, IDOK);
end;
