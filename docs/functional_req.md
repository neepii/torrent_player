# Table of Contents
1.  [GUIDELINES FOR WRITING DOCUMENTATION](#orgb5061d7)
2.  [REQUIREMNENTS](#org1ac247b)
    1.  [FRONTEND](#org71e41b4)
    2.  [BACKEND](#org2f91122)
    3.  [TEST](#orgdd9cd8f)



<a id="orgb5061d7"></a>
# GUIDELINES FOR WRITING DOCUMENTATION
-   Write in english (healthy for agents)
-   Use RFC 2119
    The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL
    NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED",  "MAY", and
    "OPTIONAL" in this document are to be interpreted as described in
    RFC 2119.


<a id="org1ac247b"></a>
# REQUIREMNENTS
<a id="org71e41b4"></a>
## FRONTEND
-   **FR-001:** The system ****shall**** accept files with .torrent file extension
-   **FR-002:** The system ****required**** to log results of bencode parsing
-   **FR-003:** The system ****required**** display an error when parser fails
-   **FR-004:** The system ****required**** display an error when HTTP GET request to tracker fails
-   **FR-005:** The system ****required**** log results of HTTP GET request to tracker
-   **FR-006:** The system ****required**** log the number and the list of peers from HTTP GET request to tracker
-   **FR-007:** The system ****required**** log the metadata after getting one
<a id="org2f91122"></a>
## BACKEND
-   **FR-008:** The system ****shall**** parse .torrent file to get structure described in Architecture
-   **FR-009:** The system ****shall**** get IP of peers after parsing .torrent file
-   **FR-010:** The system ****shall**** create unix IPC socket for media player
-   **FR-011:** The FFMPEG (See 2. in Links) ****should**** make connection to created unix IPC socket
-   **FR-012:** The system ****should**** not seed to swarm
-   **FR-013:** The system ****shall**** request pieces sequentially from peers
-   **FR-014:** The system ****should**** try distribute pieces request across all peers using ****Piece pipelining****
-   **FR-015:** The system ****shall**** create a TCP packet with video data after 5 pieces, using H.264 codec and fMP4 containter format
<a id="orgdd9cd8f"></a>
## TEST
-   **FR-017:** The system ****should not**** be tested on real torrent
    torrent client may ban you for strange behavior (inaccurate statistics, spoofing)
-   **FR-018:** The system ****should**** be tested on a local smarm within LAN network

