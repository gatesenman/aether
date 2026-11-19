@echo off
REM Aether installer for Windows (Python 3.9+ required)
python -m venv "%USERPROFILE%\.aether"
"%USERPROFILE%\.aether\Scripts\pip" install --no-index dist\aether_lang-0.2.0-py3-none-any.whl
echo Installed. Run: "%USERPROFILE%\.aether\Scripts\aether" studio
