-- MySQL dump 10.13  Distrib 8.0.46, for Linux (x86_64)
--
-- Host: 127.0.0.1    Database: file_server
-- ------------------------------------------------------
-- Server version	8.0.46-0ubuntu0.24.04.4

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Current Database: `file_server`
--

CREATE DATABASE /*!32312 IF NOT EXISTS*/ `file_server` /*!40100 DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci */ /*!80016 DEFAULT ENCRYPTION='N' */;

USE `file_server`;

--
-- Table structure for table `documents`
--

DROP TABLE IF EXISTS `documents`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `documents` (
  `id` int NOT NULL AUTO_INCREMENT,
  `file_key` varchar(64) NOT NULL,
  `file_name` varchar(255) NOT NULL DEFAULT 'untitled.md',
  `file_content` longtext,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `file_key` (`file_key`),
  KEY `idx_file_key` (`file_key`)
) ENGINE=InnoDB AUTO_INCREMENT=17 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `documents`
--

LOCK TABLES `documents` WRITE;
/*!40000 ALTER TABLE `documents` DISABLE KEYS */;
INSERT INTO `documents` VALUES (8,'1f962c24d6334374','你好文件3','你好\n\n<br />\n\n<voex-attachment data-hash=\"8158ff0f50a2bcd32341fabb35e6c4da2b09e0666c6e054f8e3f491ebc896532\" data-name=\"120.png\" data-mime=\"image%2Fpng\"></voex-attachment>\n\n','2026-08-28 17:06:38','2026-08-31 17:34:04'),(10,'1731988936424f15','xcvxc1','sdaf\n\nasdfasd\n\n<br />\n\nasdf\n\nasd\n\nf\n\nasd\n\nfas\n\ndf\n\nasd\n\nf\n\nasd\n\nf\n\nasd\n\nfas\n\ndf\n\nasd\n\nf\n\nasd\n\nfsadfasdf\n\n<br />\n\n','2026-08-28 17:15:21','2026-08-28 17:56:48'),(11,'c597112d52c74cbe','dxgdfgs1',NULL,'2026-08-28 17:15:25','2026-08-28 17:15:25'),(12,'4da440498f1f4f88','sadfasdg1的方式',NULL,'2026-08-28 17:17:31','2026-09-01 08:55:52'),(13,'606f24b2732b4666','你好文件夹','这个是一个什么文件呢\n\n<voex-attachment data-hash=\"7a89c211371c22bb515e63958d85cdfb1e21eb098018ea4554166a73b807d431\" data-name=\"618-union-title.png\" data-mime=\"image%2Fpng\"></voex-attachment>\n\n','2026-08-31 09:05:39','2026-08-31 14:35:08'),(14,'7d34fe70d9404725','你好文件夹',NULL,'2026-08-31 17:55:26','2026-08-31 17:55:26'),(15,'3f4879d929714709','邵阳菜','邵阳菜的核心追求可以概括为‌**“重口味、咸鲜香、刚烈直接”**‌，讲究食材本味与浓烈调味的碰撞。‌‌\n\n具体来说，邵阳菜的追求体现在这几个方面：\n\n* ‌**口味上追求“咸、鲜、香”**‌：邵阳地处湖南中南部，气候温暖湿润，食物不易保存，因此当地发展出了丰富的腌制食品文化。烹饪中多用盐、酱油、味精等调味品，形成了高盐高油的饮食特点，追求的是浓烈直接的味觉冲击。\n* ‌**性格上透着“硬”和“蛮”**‌：邵阳古称宝庆，码头文化孕育了“宝古佬”精神，刚毅顽强、敢闯敢拼。这种性格直接渗透到饮食里，菜品味重、分量足、口感筋道，不取悦精致味蕾，而是慰藉豪情，追求一种“吃得苦、耐得烦、霸得蛮”的痛快。\n* ‌**风味上讲究“复合与层次”**‌：很多名菜都在追求多重味觉的融合。比如血酱鸭，入口咸辣，回味中带着甜酱的醇厚、仔姜的辛香和鸭血的滑嫩；武冈卤菜独创“药卤”工艺，用二十多味中草药配伍，追求卤香层层递进、咸香之外还有回甘。\n* ‌**食材上偏好“腊味与烟熏”**‌：因为食物不易保存，邵阳人擅长用烟熏和腌制来转化食材，比如猪血丸子、腊肉等。这种处理方式赋予了食物独特的腊香和烟火气，成为邵阳菜辨识度很高的一大特色。‌‌\n\n所以，邵阳菜追求的不是精致和清淡，而是一种‌**热烈、直接、有故事、有性格的浓烈滋味**‌。\n','2026-09-02 14:46:51','2026-09-02 14:46:56'),(16,'e5b92ef2a29b4fa3','你好','谢谢光顾，现给您发货，请查收\n\n您的卡卷码：72GN196P25Q17\n\n请您观看教程去兑换会员！↓↓（教程不看领取不到概不负责）\n\n【卡券使用教程】：<https://pan.quark.cn/s/eda8e80dfd17>\n\n⚠️无法兑换的，先说明情况，再发截图给我\n⚠️教程共三步，不看教程不负责\n⚠️搜索词“好好福袋”，一定不能错\n⚠️步骤二界面不一样的话，直接点进去就行\n\n温馨提示：\n有宝子反馈第一天为打卡界面，如果出现这种情况，必须第二天第三天再去领，一定能领到https\\://mimo.xiaomimimo.com/desktop/invite/\\\n<https://github.com/sky22333/skyadb/releases/download/v1.1.5/app-release.apk>\n\n<https://post.smzdm.com/zz/p/awo2d9w2/>\n','2026-09-04 10:26:01','2026-09-08 21:55:43');
/*!40000 ALTER TABLE `documents` ENABLE KEYS */;
UNLOCK TABLES;

--
-- Table structure for table `files`
--

DROP TABLE IF EXISTS `files`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `files` (
  `id` int NOT NULL AUTO_INCREMENT,
  `file_key` varchar(64) NOT NULL,
  `hash` varchar(128) NOT NULL,
  `name` varchar(255) NOT NULL,
  `mime` varchar(128) NOT NULL,
  `size` bigint NOT NULL DEFAULT '0',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_file_key` (`file_key`),
  KEY `idx_hash` (`hash`)
) ENGINE=InnoDB AUTO_INCREMENT=18 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `files`
--

LOCK TABLES `files` WRITE;
/*!40000 ALTER TABLE `files` DISABLE KEYS */;
INSERT INTO `files` VALUES (7,'606f24b2732b4666','4e540ec7fd415ee024f44f5e92e19efb7e8576faf7440253626ad7fc4613dd41','94fc1c21628010ff4e05c2a13d02ae131638275588c9e5af4c2ceb03735a3795.webp','image/webp',35050,'2026-08-31 09:46:38'),(8,'606f24b2732b4666','7a89c211371c22bb515e63958d85cdfb1e21eb098018ea4554166a73b807d431','618-union-title.png','image/png',14627,'2026-08-31 09:50:33'),(12,'1f962c24d6334374','1ff463349d939d8d6a6aaec5511110e4ed186381b4054ad6d0074efa6e9ee1dd','activity-right-1.png','image/png',2792,'2026-08-31 14:06:44'),(15,'1f962c24d6334374','8158ff0f50a2bcd32341fabb35e6c4da2b09e0666c6e054f8e3f491ebc896532','120.png','image/png',5809,'2026-08-31 14:19:09'),(16,'7d34fe70d9404725','e1d58e3616ebd0c7ece73050631f7b4f6f3b30972fb066eba15067796b917375','IMG_20260830_213010.jpg','image/jpeg',6790453,'2026-08-31 17:55:47'),(17,'1f962c24d6334374','6b660998d37bebf01346f547578bf2faa986236a544a410dd3b9e1250b27bd86','dzfp_25432000000148115108_卢敏琪 陈睿_20251102170654.pdf','application/pdf',142394,'2026-08-31 18:22:35');
/*!40000 ALTER TABLE `files` ENABLE KEYS */;
UNLOCK TABLES;
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-09-09  9:34:37
