import { Module } from '@nestjs/common';
import { TemplateRegistrationService } from './template-registration.service';
import { RegistrationStateController, PipelineTemplateController, ConfigController } from './template-registration.controller';

@Module({
  providers: [TemplateRegistrationService],
  controllers: [RegistrationStateController, PipelineTemplateController, ConfigController],
  exports: [TemplateRegistrationService],
})
export class TemplateRegistrationModule {}
