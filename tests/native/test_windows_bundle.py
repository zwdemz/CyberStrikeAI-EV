"""Offline packaging tests: no downloaded executables or external requests."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

SCRIPT = Path(__file__).resolve().parents[2] / 'scripts/windows_bundle.py'
spec=importlib.util.spec_from_file_location('bundle',SCRIPT)
bundle=importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)

class BundleTests(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root=Path(self.directory.name)
    def archive(self, content=None, extra=None):
        content=content or {'bin/cyberstrike-ai.exe':b'MZ fixture', 'web/test.txt':b'new'}
        manifest={'format':1,'platform':'windows','revision':'a'*40,'files':{name:bundle.digest(data) for name,data in content.items()}}
        path=self.root/'bundle.zip'
        with zipfile.ZipFile(path,'w') as z:
            for name,data in content.items():z.writestr(name,data)
            z.writestr(bundle.MANIFEST,json.dumps(manifest))
            if extra:z.writestr(extra,b'bad')
        return path,bundle.digest(path.read_bytes())
    def test_archive_validation(self):
        path,sha=self.archive()
        self.assertEqual(len(bundle.read_bundle(path,sha)),3)
        with self.assertRaises(ValueError):bundle.read_bundle(path,'0'*64)
        for name in ['../escape','config.yaml','data/db','web/../escape','web/COn.txt','web/file:stream','web/trailing.']:
            path,sha=self.archive(extra=name)
            with self.assertRaises(ValueError):bundle.read_bundle(path,sha)
    def test_traversal_in_manifest(self):
        path,sha=self.archive({'../escape':b'bad'})
        with self.assertRaises(ValueError):bundle.read_bundle(path,sha)
    @unittest.skipUnless(os.name=='nt','Windows upgrade')
    def test_apply_preserve_conflict_and_rollback(self):
        target=self.root/'install';target.mkdir()
        (target/'config.yaml').write_text('private-fixture')
        path,sha=self.archive()
        (target/'.upgrade.lock').write_text('fixture')
        with self.assertRaises(FileExistsError):bundle.apply(path,sha,target)
        (target/'.upgrade.lock').unlink()
        backup=bundle.apply(path,sha,target)
        self.assertTrue(backup.is_dir())
        self.assertEqual((target/'config.yaml').read_text(),'private-fixture')
        (target/'web/test.txt').write_text('local change')
        with self.assertRaises(ValueError):bundle.apply(path,sha,target)
        (target/'web/test.txt').write_bytes(b'new')
        path,sha=self.archive({'bin/cyberstrike-ai.exe':b'MZ changed','web/test.txt':b'updated'})
        replace=bundle.os.replace
        def fail_web(src,dst):
            if str(dst).endswith('test.txt'):raise OSError('fixture failure')
            return replace(src,dst)
        with patch.object(bundle.os,'replace',side_effect=fail_web):
            with self.assertRaises(OSError):bundle.apply(path,sha,target)
        self.assertEqual((target/'bin/cyberstrike-ai.exe').read_bytes(),b'MZ fixture')
        self.assertEqual((target/'web/test.txt').read_bytes(),b'new')
    @unittest.skipUnless(os.name=='nt','Windows sharing semantics')
    def test_running_binary_rejected(self):
        binary=self.root/'service.exe'
        shutil.copy2(os.environ['COMSPEC'],binary)
        process=subprocess.Popen([str(binary),'/d','/q'],stdin=subprocess.PIPE,creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            with self.assertRaises(OSError):
                with bundle.stopped_service(binary):pass
        finally:
            process.kill();process.wait();process.stdin.close()
        with bundle.stopped_service(binary):
            self.assertGreater(len(binary.read_bytes()),0)
            with self.assertRaises(OSError):subprocess.Popen([str(binary),'/c','exit'])
    @unittest.skipUnless(os.name=='nt','PowerShell installer')
    def test_installer_hash_and_no_overwrite(self):
        root=self.root/'installation';(root/'tools').mkdir(parents=True)
        (root/'tools/ffuf.yaml').write_text('name: ffuf')
        source=Path(os.environ['COMSPEC'])
        sha=bundle.digest(source.read_bytes())
        script=SCRIPT.with_name('install-windows-tool.ps1')
        command=['powershell.exe','-NoProfile','-File',str(script),'-Name','ffuf','-Source',str(source),'-InstallRoot',str(root),'-Sha256']
        self.assertNotEqual(subprocess.run(command+['0'*64],capture_output=True).returncode,0)
        self.assertFalse((root/'tools/runtime/bin/ffuf.exe').exists())
        installed=subprocess.run(command+[sha],capture_output=True)
        self.assertEqual(installed.returncode,0,installed.stderr)
        self.assertEqual(bundle.digest((root/'tools/runtime/bin/ffuf.exe').read_bytes()),sha)
        self.assertNotEqual(subprocess.run(command+[sha],capture_output=True).returncode,0)
    def test_build_excludes_runtime_files(self):
        repo=self.root/'repo';repo.mkdir()
        for name in ['config.example.yaml','config.yaml','data/secrets','run.ps1','scripts/windows_bundle.py']:
            p=repo/name;p.parent.mkdir(exist_ok=True,parents=True);p.write_text('fixture')
        for args in [['init'],['add','.'],['-c','user.name=Fixture','-c','user.email=fixture@example.invalid','-c','commit.gpgsign=false','commit','-m','fixture']]:
            subprocess.run(['git','-C',str(repo),*args],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        binary=self.root/'service.exe';binary.write_bytes(b'MZ fixture')
        output=self.root/'release.zip'
        bundle.build(repo,binary,output)
        with zipfile.ZipFile(output) as z:
            self.assertNotIn('config.yaml',z.namelist());self.assertNotIn('data/secrets',z.namelist())

if __name__=='__main__':unittest.main()
