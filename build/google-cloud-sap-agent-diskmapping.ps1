<#
.SYNOPSIS
  Agent for SAP physical disk mapping script.
.DESCRIPTION
  This powershell script is used to map Google Cloud disk names to Windows
  physical disk names. 'Google  mydisk' -> '\\.\PHYSICALDISKX'
#>

param (
  [parameter(Mandatory=$true)]
  [string] $diskName
)

$PhysicalDisksClass = @"
using System;
using Microsoft.Win32.SafeHandles;
using System.IO;
using System.Runtime.InteropServices;
using System.Threading;
public class PhysicalDisks {
  private const int PropertyStandardQuery = 0;
  private const int StorageDeviceIdProperty = 2;
  private const int ERROR_INSUFFICIENT_BUFFER = 122;
  private const int StorageIdTypeVendorId = 1;
  private const uint IOCTL_STORAGE_QUERY_PROPERTY = 0x2d1400;
  [StructLayout(LayoutKind.Sequential)]
  public struct StoragePropertyQuery {
    public uint PropertyId;
    public uint QueryType;
    [MarshalAs(UnmanagedType.ByValArray, SizeConst = 1)]
    public byte[] AdditionalParameters;
  }
  [DllImport("kernel32.dll", SetLastError = true)]
  private static extern SafeFileHandle CreateFile(
    string lpFileName,
    [MarshalAs(UnmanagedType.U4)] FileAccess dwDesiredAccess,
    [MarshalAs(UnmanagedType.U4)] FileShare dwShareMode,
    IntPtr lpSecurityAttributes,
    [MarshalAs(UnmanagedType.U4)] FileMode dwCreationDisposition,
    [MarshalAs(UnmanagedType.U4)] FileAttributes dwFlagsAndAttributes,
    IntPtr hTemplateFile);
  [DllImport("kernel32.dll", SetLastError = true)]
  [return: MarshalAs(UnmanagedType.Bool)]
  private static extern bool DeviceIoControl(
    SafeFileHandle hDevice,
    uint IoControlCode,
    ref StoragePropertyQuery inBuffer,
    int nInBufferSize,
    IntPtr outBuffer,
    int nOutBufferSize,
    ref uint pBytesReturned,
    IntPtr Overlapped
    );
  [StructLayout(LayoutKind.Sequential)]
  public struct StorageIdentifier {
    public int CodeSet;
    public int Type;
    public ushort IdentifierSize;
    public ushort NextOffset;
    public int Association;
    [MarshalAs(UnmanagedType.ByValArray, SizeConst = 1)]
    public byte[] Identifier;
  }
  [StructLayout(LayoutKind.Sequential)]
  public struct StorageDeviceIdDescriptor {
    public uint Version;
    public uint Size;
    public uint NumberOfIdentifiers;
    [MarshalAs(UnmanagedType.ByValArray, SizeConst = 1)]
    public byte[] Identifiers;
  }
  public static string queryPage83(string disk) {
    disk = @"\\.\" + disk;
    SafeFileHandle hDisk = CreateFile(disk, FileAccess.Read | FileAccess.Write, FileShare.Read | FileShare.Write, IntPtr.Zero, FileMode.Open, 0, IntPtr.Zero);
    if (hDisk == null || hDisk.IsInvalid) {
      return "";
    }
    StoragePropertyQuery spq = new StoragePropertyQuery();
    spq.PropertyId = StorageDeviceIdProperty;
    spq.QueryType = PropertyStandardQuery;
    int buflen = 8192;
    IntPtr bufptr = IntPtr.Zero;
    uint bytesReturned = new uint();
    do {
      bufptr = Marshal.AllocHGlobal(buflen);
      bool result = DeviceIoControl(hDisk, IOCTL_STORAGE_QUERY_PROPERTY, ref spq, Marshal.SizeOf(spq), bufptr, buflen, ref bytesReturned, IntPtr.Zero);
      if (!result) {
        int error = Marshal.GetLastWin32Error();
        if (error == ERROR_INSUFFICIENT_BUFFER) {
          Marshal.FreeHGlobal(bufptr);
          bufptr = IntPtr.Zero;
          buflen *= 2;
          continue;
        }
        hDisk.Close();
        return "";
      }
      break;
    } while(true);
    hDisk.Close();
    IntPtr ptr = IntPtr.Add(bufptr, (int) Marshal.OffsetOf(typeof(StorageDeviceIdDescriptor), "NumberOfIdentifiers"));
    uint n = (uint) Marshal.ReadInt32(ptr);
    ptr = IntPtr.Add(bufptr, (int) Marshal.OffsetOf(typeof(StorageDeviceIdDescriptor), "Identifiers"));
    for (uint i = 0; i < n; i++) {
      int idType = Marshal.ReadInt32(IntPtr.Add(ptr, (int) Marshal.OffsetOf(typeof(StorageIdentifier), "Type")));
      if (idType == StorageIdTypeVendorId) {
        string id = Marshal.PtrToStringAnsi(IntPtr.Add(ptr, (int) Marshal.OffsetOf(typeof(StorageIdentifier), "Identifier")));
        Marshal.FreeHGlobal(bufptr);
        return id;
      }
      ptr = ptr + Marshal.ReadInt16(IntPtr.Add(ptr, (int) Marshal.OffsetOf(typeof(StorageIdentifier), "NextOffset")));
    }
    Marshal.FreeHGlobal(bufptr);
    return "";
  }
}
"@

# Compile and register C# PhysicalDisks class
Add-Type -TypeDefinition $PhysicalDisksClass -ErrorAction Stop
function Get-PhysicalDiskMapping {
  param (
    [string]$diskName
  )
  [Array] $physicalDisks = @(Get-PhysicalDisk)
  for ($i = 0; $i -lt $physicalDisks.length; $i++) {
    $drive = 'PhysicalDrive' + $physicalDisks[$i].DeviceId
    $id = [PhysicalDisks]::queryPage83($drive)
    if ($id -eq $diskName) {
      return 'PhysicalDrive' + $physicalDisks[$i].DeviceId
    }
  }
  return ''
}
Write-Host $(Get-PhysicalDiskMapping ('Google  ' + $diskName))
